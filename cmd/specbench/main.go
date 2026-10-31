// Command specbench measures speculative decoding on this computer (#835):
// it starts llama-server with a model, plain and with each kind of
// guessing ahead, asks the same prompts at temperature 0, and reports the
// speed, how many guesses were kept, and whether each answer is the plain
// one's. Where an answer differs, it says whether it differs only at a
// near-tie: the first word it changed was, in the plain run, within
// tieNats of the word the plain run wrote. That's what batched checking
// can flip, and it's no worse an answer.
//
//	go run ./cmd/specbench -llama /path/to/llama-server -model big.gguf -draft small.gguf -out report.md
//
// Beside a Toskar daemon, as in the quality workflow's container, it can
// have the daemon install llama.cpp and the model first, and download the
// draft:
//
//	specbench -toskar http://127.0.0.1:7331 -data /data -install qwen2.5-32b-q4 \
//	  -draft /data/models/drafts/qwen2.5-0.5b.gguf -draft-url https://…/qwen2.5-0.5b-instruct-q4_k_m.gguf
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// tieNats is how close, in log-probability, two words are to count as a
// tie that batched arithmetic may flip.
const tieNats = 0.05

type config struct {
	name string
	args []string
}

type prompt struct{ id, text string }

// prompts are short and long, new text and edits of text in the chat, so
// both kinds of guessing get a fair chance.
var prompts = []prompt{
	{"fact", "What is the capital of Australia? Answer in one sentence."},
	{"explain", "Explain how a refrigerator keeps food cold, in about 200 words."},
	{"code", "Write a Python function that parses a CSV file of orders (id, customer, amount, date) and returns the total amount per customer, with type hints and a docstring."},
	{"list", "List the 20 largest cities in Europe by population, one per line, as 'City, Country'."},
	{"translate", "Translate into German: 'The museum opens at nine, but the cafe on the top floor only opens at eleven. Tickets are cheaper online.'"},
	{"plan", "Plan a three-day trip to Juneau, Alaska in September, with a short paragraph for each day."},
	{"story", "Write a short story, about 500 words, about a lighthouse keeper who finds a message in a bottle."},
	{"edit-code", "Here is some Python code:\n\n```python\ndef total_per_customer(path):\n    totals = {}\n    with open(path) as f:\n        next(f)\n        for line in f:\n            order_id, customer, amount, date = line.strip().split(\",\")\n            totals[customer] = totals.get(customer, 0) + float(amount)\n    return totals\n\n\ndef largest_order(path):\n    best = None\n    with open(path) as f:\n        next(f)\n        for line in f:\n            order_id, customer, amount, date = line.strip().split(\",\")\n            if best is None or float(amount) > best[1]:\n                best = (order_id, float(amount))\n    return best\n```\n\nAdd type hints and a one-line docstring to both functions, and return the whole file."},
	{"edit-letter", "Change the visit to Thursday 16 October, 1 pm to 4 pm, and return the whole letter:\n\nDear Ms. Alvarez,\n\nThank you for your letter of 3 October about the heating in flat 4B. I am sorry the radiators in the bedroom and the living room have been cold since the start of the month. Our engineer, Tom Reyes, will visit on Tuesday 14 October between 9 am and 12 noon to bleed the radiators and check the boiler pressure. If that time does not suit you, please call the office on 01632 960 431 and we will arrange another day.\n\nWhile you wait, you are welcome to collect an electric heater from the office, free of charge. Please bring this letter with you.\n\nKind regards,\nPriya Natarajan\nProperty Manager"},
}

type answer struct {
	Text    string  `json:"text"`
	TokSec  float64 `json:"tok_per_sec"`
	Drafted int     `json:"drafted"`
	Kept    int     `json:"kept"`
	tokens  []token
}

type token struct {
	Token string `json:"token"`
	Top   []struct {
		Token   string  `json:"token"`
		Logprob float64 `json:"logprob"`
	} `json:"top_logprobs"`
}

// verdict is how an answer compares with the plain one.
type verdict struct {
	Same    bool    `json:"same"`
	NearTie bool    `json:"near_tie,omitempty"`
	Gap     float64 `json:"gap,omitempty"`
	At      string  `json:"at,omitempty"`
}

func main() {
	llama := flag.String("llama", "", "llama-server to run")
	model := flag.String("model", "", "the model, a GGUF file")
	draft := flag.String("draft", "", "a draft model of the same family, a GGUF file; without it, only guessing from the chat is measured")
	port := flag.Int("port", 7471, "port for llama-server (not 7331, where Toskar may run)")
	ctxSize := flag.Int("ctx", 8192, "context size")
	gpuLayers := flag.String("ngl", "999", "layers on the GPU")
	maxTokens := flag.Int("max-tokens", 600, "longest answer")
	out := flag.String("out", "", "write the report here (Markdown) as well as to stdout")
	jsonOut := flag.String("json", "", "write every answer here")
	toskar := flag.String("toskar", "", "a Toskar daemon to install llama.cpp and -install's model with (TOSKAR_API_KEY is its key)")
	install := flag.String("install", "", "with -toskar: the catalog model to install and measure")
	dataDir := flag.String("data", "/data", "with -install: the daemon's data folder, for llama-server and the model's file")
	draftURL := flag.String("draft-url", "", "download the draft from here when -draft isn't there yet")
	flag.Parse()
	if *toskar != "" && *install != "" {
		if err := prepare(*toskar, *install); err != nil {
			log.Fatal(err)
		}
		if *llama == "" {
			*llama = filepath.Join(*dataDir, "runtimes", "llamacpp", "llama-server")
		}
		if *model == "" {
			*model = filepath.Join(*dataDir, "models", *install+".gguf")
		}
	}
	if *draft != "" && *draftURL != "" {
		if err := download(*draftURL, *draft); err != nil {
			log.Fatal(err)
		}
	}
	if *llama == "" || *model == "" {
		flag.Usage()
		os.Exit(2)
	}
	configs := []config{{"off", nil}, {"ngram-mod", []string{"--spec-type", "ngram-mod"}}}
	if *draft != "" {
		d := []string{"-md", *draft, "-ngld", *gpuLayers}
		configs = append(configs,
			config{"draft", append(d, "--spec-type", "draft-simple")},
			config{"draft+ngram-mod", append(append([]string{}, d...), "--spec-type", "draft-simple,ngram-mod")})
	}

	results := map[string]map[string]answer{}
	for _, c := range configs {
		log.Printf("%s: starting llama-server", c.name)
		got, err := run(*llama, *model, *port, *ctxSize, *gpuLayers, *maxTokens, c)
		if err != nil {
			log.Fatalf("%s: %v", c.name, err)
		}
		results[c.name] = got
	}
	report := write(*model, *draft, configs, results)
	fmt.Print(report)
	if *out != "" {
		if err := os.WriteFile(*out, []byte(report), 0o644); err != nil {
			log.Fatal(err)
		}
	}
	if *jsonOut != "" {
		b, _ := json.MarshalIndent(results, "", " ")
		if err := os.WriteFile(*jsonOut, b, 0o644); err != nil {
			log.Fatal(err)
		}
	}
}

// run starts llama-server with c, asks every prompt, and stops it. The
// plain run also asks for each token's top two choices, which llama-server
// answers without guessing ahead, so speed comes from a separate ask.
func run(llama, model string, port, ctxSize int, ngl string, maxTokens int, c config) (map[string]answer, error) {
	args := append([]string{"-m", model, "--port", fmt.Sprint(port), "--ctx-size", fmt.Sprint(ctxSize),
		"--parallel", "1", "-ngl", ngl, "--jinja"}, c.args...)
	cmd := exec.Command(llama, args...)
	var logs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &logs, &logs
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	if err := ready(base, cmd); err != nil {
		return nil, fmt.Errorf("%w\n%s", err, tail(logs.String(), 2000))
	}
	if _, err := ask(base, "Say hi.", maxTokens, false); err != nil {
		return nil, err
	}
	got := map[string]answer{}
	for _, p := range prompts {
		a, err := ask(base, p.text, maxTokens, false)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p.id, err)
		}
		if c.name == "off" {
			probed, err := ask(base, p.text, maxTokens, true)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", p.id, err)
			}
			a.tokens = probed.tokens
		}
		log.Printf("%s %s: %.1f tok/s, kept %d of %d", c.name, p.id, a.TokSec, a.Kept, a.Drafted)
		got[p.id] = a
	}
	return got, nil
}

func ready(base string, cmd *exec.Cmd) error {
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		if cmd.ProcessState != nil {
			return errors.New("llama-server stopped")
		}
		if r, err := http.Get(base + "/health"); err == nil {
			r.Body.Close()
			if r.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return errors.New("llama-server didn't start in 10 minutes")
}

func ask(base, text string, maxTokens int, probs bool) (answer, error) {
	body := map[string]any{
		"messages":    []map[string]string{{"role": "user", "content": text}},
		"temperature": 0, "max_tokens": maxTokens, "seed": 1,
	}
	if probs {
		body["logprobs"], body["top_logprobs"] = true, 2
	}
	b, _ := json.Marshal(body)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/chat/completions", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		return answer{}, err
	}
	defer r.Body.Close()
	raw, _ := io.ReadAll(r.Body)
	if r.StatusCode != http.StatusOK {
		return answer{}, fmt.Errorf("llama-server: %s: %s", r.Status, tail(string(raw), 500))
	}
	var resp struct {
		Choices []struct {
			Message  struct{ Content string } `json:"message"`
			Logprobs struct {
				Content []token `json:"content"`
			} `json:"logprobs"`
		} `json:"choices"`
		Timings struct {
			PredictedPerSecond float64 `json:"predicted_per_second"`
			DraftN             int     `json:"draft_n"`
			DraftNAccepted     int     `json:"draft_n_accepted"`
		} `json:"timings"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || len(resp.Choices) == 0 {
		return answer{}, fmt.Errorf("unexpected answer: %s", tail(string(raw), 500))
	}
	c := resp.Choices[0]
	return answer{Text: c.Message.Content, TokSec: resp.Timings.PredictedPerSecond,
		Drafted: resp.Timings.DraftN, Kept: resp.Timings.DraftNAccepted, tokens: c.Logprobs.Content}, nil
}

// compare finds the first plain token the other answer leaves, and how
// close the plain run's top two choices were there.
func compare(plain answer, other string) verdict {
	if plain.Text == other {
		return verdict{Same: true}
	}
	done := ""
	for _, t := range plain.tokens {
		if !strings.HasPrefix(other, done+t.Token) {
			v := verdict{At: done[max(0, len(done)-40):]}
			if len(t.Top) > 1 && strings.HasPrefix(other[len(done):], t.Top[1].Token) {
				v.Gap = t.Top[0].Logprob - t.Top[1].Logprob
				v.NearTie = v.Gap < tieNats
			} else {
				v.Gap = math.Inf(1)
			}
			return v
		}
		done += t.Token
	}
	// The other answer goes on past the plain one, or the plain one had no
	// log-probabilities: not a tie anyone can show.
	return verdict{Gap: math.Inf(1), At: done[max(0, len(done)-40):]}
}

func write(model, draft string, configs []config, results map[string]map[string]answer) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Speculative decoding on this computer\n\nModel `%s`", baseName(model))
	if draft != "" {
		fmt.Fprintf(&b, ", draft `%s`", baseName(draft))
	}
	fmt.Fprintf(&b, ". Temperature 0, one ask per prompt after a warm-up. Each cell is tokens per second, the change from off, guesses kept of those made, and how the answer compares with off: same, a near-tie (the first changed word was within %.2f nats of the one off wrote), or **different**.\n\n", tieNats)
	b.WriteString("| Prompt |")
	for _, c := range configs {
		fmt.Fprintf(&b, " %s |", c.name)
	}
	b.WriteString("\n|---|")
	for range configs {
		b.WriteString("---|")
	}
	b.WriteString("\n")
	off := results["off"]
	type sum struct {
		logSpeed       float64
		n, ties, diffs int
	}
	sums := map[string]*sum{}
	for _, p := range prompts {
		fmt.Fprintf(&b, "| %s |", p.id)
		for _, c := range configs {
			a := results[c.name][p.id]
			if c.name == "off" {
				fmt.Fprintf(&b, " %.1f |", a.TokSec)
				continue
			}
			s := sums[c.name]
			if s == nil {
				s = &sum{}
				sums[c.name] = s
			}
			change := 0.0
			if off[p.id].TokSec > 0 && a.TokSec > 0 {
				change = a.TokSec/off[p.id].TokSec - 1
				s.logSpeed += math.Log(a.TokSec / off[p.id].TokSec)
				s.n++
			}
			v := compare(off[p.id], a.Text)
			how := "same"
			switch {
			case v.NearTie:
				how = fmt.Sprintf("near-tie (%.3f)", v.Gap)
				s.ties++
			case !v.Same:
				how = "**different**"
				s.diffs++
			}
			fmt.Fprintf(&b, " %.1f (%+.0f%%), %d/%d, %s |", a.TokSec, change*100, a.Kept, a.Drafted, how)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	for _, c := range configs[1:] {
		s := sums[c.name]
		if s == nil || s.n == 0 {
			continue
		}
		fmt.Fprintf(&b, "- **%s**: %+.0f%% on average (geometric mean); %d near-ties, %d different.\n",
			c.name, (math.Exp(s.logSpeed/float64(s.n))-1)*100, s.ties, s.diffs)
	}
	return b.String()
}

func baseName(p string) string { return p[strings.LastIndex(p, "/")+1:] }

func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// prepare has the daemon at base install llama.cpp and the model, and
// waits for the model's download.
func prepare(base, id string) error {
	call := func(method, path string, out any) error {
		req, _ := http.NewRequest(method, base+path, nil)
		if key := os.Getenv("TOSKAR_API_KEY"); key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer r.Body.Close()
		raw, _ := io.ReadAll(r.Body)
		if r.StatusCode >= 300 {
			return fmt.Errorf("%s %s: %s: %s", method, path, r.Status, tail(string(raw), 300))
		}
		if out != nil {
			return json.Unmarshal(raw, out)
		}
		return nil
	}
	log.Printf("installing llama.cpp")
	if err := call(http.MethodPost, "/api/v1/runtimes/llamacpp/install", nil); err != nil {
		return err
	}
	type model struct {
		ID        string `json:"id"`
		Installed bool   `json:"installed"`
		Status    string `json:"status"`
	}
	find := func() (model, error) {
		var list []model
		if err := call(http.MethodGet, "/api/v1/models", &list); err != nil {
			return model{}, err
		}
		for _, m := range list {
			if m.ID == id {
				return m, nil
			}
		}
		return model{}, fmt.Errorf("no model %s in the catalog", id)
	}
	m, err := find()
	if err != nil || m.Installed {
		return err
	}
	log.Printf("installing %s", id)
	if err := call(http.MethodPost, "/api/v1/models/"+id+"/install", nil); err != nil {
		return err
	}
	for deadline := time.Now().Add(3 * time.Hour); time.Now().Before(deadline); time.Sleep(15 * time.Second) {
		m, err := find()
		switch {
		case err != nil:
			return err
		case m.Installed:
			return nil
		case m.Status == "error":
			return fmt.Errorf("installing %s failed", id)
		}
	}
	return fmt.Errorf("%s still installing after 3 hours", id)
}

// download fetches url to path, unless path is there already.
func download(url, path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	log.Printf("downloading %s", url)
	r, err := http.Get(url)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", url, r.Status)
	}
	part := path + ".part"
	f, err := os.Create(part)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r.Body); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(part, path)
}
