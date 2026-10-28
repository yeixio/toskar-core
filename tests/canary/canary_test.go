package canary

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// The llama.cpp canary (#833). Toskar installs the newest llama.cpp release,
// and nothing else checks that a new release still computes the same
// numbers on the GPU backends people run: a backend bug reaches them as
// worse answers with no error. llama-perplexity scores a fixed text with a
// small fixed model (goldens.json) and compares a release's next-token
// probabilities with the known-good tag's:
//
//   - Golden: the good tag still scores the text as it did when the
//     goldens were made, on this lane (a backend on a kind of hardware).
//     A miss means the machine changed, such as a new graphics driver.
//   - Release: the new release on the same backend matches the good tag:
//     mean KL divergence, how often the top token is the same, and the
//     perplexity change, within the lane's tolerances.
//   - CPU: on a GPU lane, the new release on the CPU matches the good tag
//     on the GPU, within the looser cross-backend tolerances, so a GPU
//     backend that drifts on both tags is caught too.
//
// It runs where scripts/llama-canary.sh sets it up, and is skipped
// without it.
//
//	TOSKAR_CANARY_GOOD    directory of the good tag's llama-perplexity
//	TOSKAR_CANARY_NEW     directory of the new release's llama-perplexity
//	TOSKAR_CANARY_MODEL   the model file (goldens.json's model)
//	TOSKAR_CANARY_LANE    the lane's name in goldens.json, such as metal-arm64
//	TOSKAR_CANARY_GPU     "1" when the lane runs on a GPU
//	TOSKAR_CANARY_EXEC    a command to run llama-perplexity through, such as docker run …
//	TOSKAR_CANARY_WORK    where the good tag's probabilities go (about 1.5 GB)
//	TOSKAR_CANARY_REPORT  writes the measurements as Markdown
//	TOSKAR_CANARY_UPDATE  "1" writes this lane's golden to goldens.json, for a reviewed PR

// Goldens is goldens.json.
type Goldens struct {
	// GoodTag is the llama.cpp release the others are compared with.
	GoodTag string `json:"good_tag"`
	Model   struct {
		URL    string `json:"url"`
		SHA256 string `json:"sha256"`
	} `json:"model"`
	// Context is the window each chunk of the text is scored in.
	Context int `json:"context"`
	// Release holds a release to the good tag on the same backend; CPU
	// holds the CPU to the good tag on a GPU.
	Release Tolerance `json:"release"`
	CPU     Tolerance `json:"cpu"`
	// Lanes are the good tag's perplexity per lane, with any lane's own
	// tolerances.
	Lanes map[string]Lane `json:"lanes"`
}

// Tolerance is how far a run may be from its base.
type Tolerance struct {
	// MeanKLD is the most mean KL divergence, in nats.
	MeanKLD float64 `json:"mean_kld"`
	// SameTop is the least share of tokens whose top choice is the base's, 0 to 1.
	SameTop float64 `json:"same_top"`
	// PPL is the most |ln(PPL/PPL base)|.
	PPL float64 `json:"ppl"`
}

// Lane is one backend on one kind of hardware.
type Lane struct {
	// PPL is the good tag's perplexity of the text here.
	PPL float64 `json:"ppl"`
	// What says what the lane ran on when the golden was made.
	What    string     `json:"what,omitempty"`
	Release *Tolerance `json:"release,omitempty"`
	CPU     *Tolerance `json:"cpu,omitempty"`
}

// Stats is what llama-perplexity --kl-divergence reports.
type Stats struct {
	PPL      float64
	LnRatio  float64
	MeanKLD  float64
	KLD99    float64
	MaxKLD   float64
	SameTop  float64 // 0 to 1
	RMSDelta float64 // percent
}

var (
	finalPPL = regexp.MustCompile(`Final estimate: PPL = ([0-9.]+)`)
	statLine = regexp.MustCompile(`(?m)^\s*([^:\n]+?)\s*:\s*(-?[0-9.]+)`)
)

// parseBase reads the perplexity a --kl-divergence-base run prints.
func parseBase(out string) (float64, error) {
	m := finalPPL.FindStringSubmatch(out)
	if m == nil {
		return 0, errors.New("no final perplexity in llama-perplexity's output")
	}
	return strconv.ParseFloat(m[1], 64)
}

// parseKLD reads the statistics a --kl-divergence run prints.
func parseKLD(out string) (Stats, error) {
	i := strings.Index(out, "====== Perplexity statistics")
	if i < 0 {
		return Stats{}, errors.New("no statistics in llama-perplexity's output")
	}
	vals := map[string]float64{}
	for _, m := range statLine.FindAllStringSubmatch(out[i:], -1) {
		v, err := strconv.ParseFloat(m[2], 64)
		if err == nil {
			vals[strings.Join(strings.Fields(m[1]), " ")] = v
		}
	}
	s := Stats{
		PPL:      vals["Mean PPL(Q)"],
		LnRatio:  vals["Mean ln(PPL(Q)/PPL(base))"],
		MeanKLD:  vals["Mean KLD"],
		KLD99:    vals["99.0% KLD"],
		MaxKLD:   vals["Maximum KLD"],
		SameTop:  vals["Same top p"] / 100,
		RMSDelta: vals["RMS Δp"],
	}
	for _, k := range []string{"Mean PPL(Q)", "Mean KLD", "Same top p"} {
		if _, ok := vals[k]; !ok {
			return Stats{}, fmt.Errorf("no %q in llama-perplexity's output", k)
		}
	}
	return s, nil
}

// check says how a run misses its tolerance, or "" when it doesn't.
func (s Stats) check(tol Tolerance) string {
	var miss []string
	if s.MeanKLD > tol.MeanKLD {
		miss = append(miss, fmt.Sprintf("mean KL divergence %.6f is over %.6f", s.MeanKLD, tol.MeanKLD))
	}
	if s.SameTop < tol.SameTop {
		miss = append(miss, fmt.Sprintf("the top token is the same for %.2f%%, under %.2f%%", 100*s.SameTop, 100*tol.SameTop))
	}
	if math.Abs(s.LnRatio) > tol.PPL {
		miss = append(miss, fmt.Sprintf("perplexity changed by %+.3f%%, more than %.3f%%", 100*math.Expm1(s.LnRatio), 100*math.Expm1(tol.PPL)))
	}
	return strings.Join(miss, "; ")
}

func readGoldens(path string) (Goldens, error) {
	var g Goldens
	b, err := os.ReadFile(path)
	if err != nil {
		return g, err
	}
	err = json.Unmarshal(b, &g)
	return g, err
}

func writeGoldens(path string, g Goldens) error {
	b, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// perplexity runs llama-perplexity from dir over the text.
func perplexity(t *testing.T, dir string, args ...string) string {
	t.Helper()
	name := "llama-perplexity"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	argv := append(strings.Fields(os.Getenv("TOSKAR_CANARY_EXEC")), filepath.Join(dir, name))
	argv = append(argv, args...)
	cmd := exec.Command(argv[0], argv[1:]...)
	// The release archives keep their shared libraries beside the programs.
	cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+dir)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s: %v\n%s", strings.Join(argv, " "), err, tail(out.String(), 40))
	}
	return out.String()
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func TestLlamaCanary(t *testing.T) {
	good, next := os.Getenv("TOSKAR_CANARY_GOOD"), os.Getenv("TOSKAR_CANARY_NEW")
	model, laneName := os.Getenv("TOSKAR_CANARY_MODEL"), os.Getenv("TOSKAR_CANARY_LANE")
	if good == "" || model == "" || laneName == "" {
		t.Skip("set TOSKAR_CANARY_GOOD, TOSKAR_CANARY_MODEL and TOSKAR_CANARY_LANE (scripts/llama-canary.sh does)")
	}
	gpu := os.Getenv("TOSKAR_CANARY_GPU") == "1"
	update := os.Getenv("TOSKAR_CANARY_UPDATE") == "1"
	g, err := readGoldens("goldens.json")
	if err != nil {
		t.Fatal(err)
	}
	text, err := filepath.Abs("text.txt")
	if err != nil {
		t.Fatal(err)
	}
	work := os.Getenv("TOSKAR_CANARY_WORK")
	if work == "" {
		work = t.TempDir()
	}
	base := filepath.Join(work, "base.kld")
	defer os.Remove(base)
	// On the CPU, no device at all: with no layers on a GPU, llama.cpp
	// still sends large batches to it (op offload), as it did on Vulkan.
	cpuOnly := []string{"-ngl", "0", "--device", "none"}
	backend := cpuOnly
	if gpu {
		backend = []string{"-ngl", "99"}
	}
	common := []string{"-m", model, "-f", text, "-c", strconv.Itoa(g.Context)}

	r := report{Lane: laneName, GoodTag: g.GoodTag, NewTag: tagOf(next)}
	defer func() {
		if path := os.Getenv("TOSKAR_CANARY_REPORT"); path != "" {
			if err := os.WriteFile(path, []byte(r.markdown()), 0o644); err != nil {
				t.Error(err)
			}
		}
	}()

	// The good tag, on this lane's backend, is the base for both checks.
	out := perplexity(t, good, append(append(common, backend...), "--kl-divergence-base", base)...)
	ppl, err := parseBase(out)
	if err != nil {
		t.Fatalf("%v\n%s", err, tail(out, 40))
	}
	r.GoodPPL = ppl
	lane, ok := g.Lanes[laneName]
	switch {
	case update:
		lane.PPL = ppl
		if g.Lanes == nil {
			g.Lanes = map[string]Lane{}
		}
		g.Lanes[laneName] = lane
		if err := writeGoldens("goldens.json", g); err != nil {
			t.Fatal(err)
		}
		t.Logf("canary: wrote %s's golden, PPL %.4f with %s", laneName, ppl, g.GoodTag)
	case !ok:
		r.Notes = append(r.Notes, fmt.Sprintf("%s has no golden yet: the good tag scored PPL %.4f. Add it with scripts/llama-canary.sh refresh, in a reviewed PR.", laneName, ppl))
	default:
		r.GoldenPPL = lane.PPL
		if d := math.Abs(math.Log(ppl / lane.PPL)); d > g.Release.PPL {
			r.fail(t, "golden", fmt.Sprintf("%s scores PPL %.4f here, not the golden %.4f: this machine computes differently than when the golden was made (a driver or hardware change?)", g.GoodTag, ppl, lane.PPL))
		}
	}

	release, cpu := g.Release, g.CPU
	if lane.Release != nil {
		release = *lane.Release
	}
	if lane.CPU != nil {
		cpu = *lane.CPU
	}
	kld := append(common, "--kl-divergence-base", base, "--kl-divergence")

	if next != "" {
		out := perplexity(t, next, append(kld, backend...)...)
		s, err := parseKLD(out)
		if err != nil {
			t.Fatalf("%v\n%s", err, tail(out, 40))
		}
		r.add("release", r.NewTag+" against "+g.GoodTag, s, release)
		if miss := s.check(release); miss != "" {
			r.fail(t, "release", r.NewTag+" differs from "+g.GoodTag+": "+miss)
		}
	}
	if gpu {
		tag, dir := g.GoodTag, good
		if next != "" {
			tag, dir = r.NewTag, next
		}
		out := perplexity(t, dir, append(kld, cpuOnly...)...)
		s, err := parseKLD(out)
		if err != nil {
			t.Fatalf("%v\n%s", err, tail(out, 40))
		}
		r.add("cpu", tag+" on the CPU against "+g.GoodTag+" on the GPU", s, cpu)
		if miss := s.check(cpu); miss != "" {
			r.fail(t, "cpu", "the GPU and CPU differ: "+miss)
		}
	}
	for _, row := range r.Rows {
		t.Logf("canary %s: %s: mean KLD %.6f, same top %.2f%%, PPL %+.3f%%", laneName, row.What, row.Stats.MeanKLD, 100*row.Stats.SameTop, 100*math.Expm1(row.Stats.LnRatio))
	}
}

// tagOf reads a release tag, such as b11946, from its directory's name.
func tagOf(dir string) string {
	if dir == "" {
		return ""
	}
	m := regexp.MustCompile(`b[0-9]+`).FindAllString(dir, -1)
	if m == nil {
		return filepath.Base(dir)
	}
	return m[len(m)-1]
}

type report struct {
	Lane, GoodTag, NewTag string
	GoodPPL, GoldenPPL    float64
	Rows                  []reportRow
	Failed                []string
	Notes                 []string
}

type reportRow struct {
	Check, What string
	Stats       Stats
	Tol         Tolerance
}

func (r *report) add(check, what string, s Stats, tol Tolerance) {
	r.Rows = append(r.Rows, reportRow{check, what, s, tol})
}

func (r *report) fail(t *testing.T, check, msg string) {
	t.Helper()
	r.Failed = append(r.Failed, check)
	t.Error(msg)
}

func (r report) markdown() string {
	var b strings.Builder
	status := "passed"
	if len(r.Failed) > 0 {
		status = "failed (" + strings.Join(r.Failed, ", ") + ")"
	}
	fmt.Fprintf(&b, "### llama.cpp canary: %s, %s\n\n", r.Lane, status)
	if r.GoldenPPL > 0 {
		fmt.Fprintf(&b, "%s scored PPL %.4f (golden %.4f).\n\n", r.GoodTag, r.GoodPPL, r.GoldenPPL)
	} else {
		fmt.Fprintf(&b, "%s scored PPL %.4f.\n\n", r.GoodTag, r.GoodPPL)
	}
	if len(r.Rows) > 0 {
		b.WriteString("| Check | Mean KLD | 99% KLD | Max KLD | Same top | PPL change | RMS Δp |\n| --- | --- | --- | --- | --- | --- | --- |\n")
		for _, row := range r.Rows {
			s := row.Stats
			fmt.Fprintf(&b, "| %s | %.6f (≤ %g) | %.6f | %.6f | %.2f%% (≥ %g%%) | %+.3f%% | %.3f%% |\n",
				row.What, s.MeanKLD, row.Tol.MeanKLD, s.KLD99, s.MaxKLD, 100*s.SameTop, 100*row.Tol.SameTop, 100*math.Expm1(s.LnRatio), s.RMSDelta)
		}
		b.WriteString("\n")
	}
	for _, n := range r.Notes {
		b.WriteString(n + "\n\n")
	}
	return b.String()
}
