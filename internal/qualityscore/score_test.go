package qualityscore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These sentences are the real reports' words, from a weekly run.
const onCard = "Tested Toskar 0.1.0-dev (a4333f9) on AMD Radeon RX 7900 XT/7900 XTX/7900 GRE/7900M (24 GB), with %s; the model ran on the GPU (Vulkan, 43 of 43 layers).\n\n"

func TestScoreOfReadsEachReportsOwnSentence(t *testing.T) {
	gpu := fmt.Sprintf(onCard, "m")
	for _, c := range []struct {
		set, report string
		want        Passed
	}{
		{"quality", gpu + "60 of 64 cases passed (94%, 95% interval 85–98%); at least 90% must pass.\n\n**Covered 64 of 77 cases**, so this isn't a full run.\n", Passed{60, 64}},
		// The topic controls' two numbers leave out cases that failed some other way.
		{"topics", gpu + "The topic controls held 13 of 13 off-topic cases (100%); wrongly refused 0 of 9 on-topic ones (0%).\n\n20 of 22 cases passed (91%, 95% interval 70–98%).\n", Passed{20, 22}},
		{"files", gpu + "20 of 21 cases passed (95%, 95% interval 77–99%).\n", Passed{20, 21}},
		{"reasoning", gpu + "32 of 39 right (82%), 1.0 s and 388 tokens a question on average.\n", Passed{32, 39}},
		{"triage", gpu + "type right on 26 of 30, precision 62%, recall 67%, 1.5 s an issue (30 answered of 30).\n", Passed{26, 30}},
	} {
		if got, ok := ScoreOf(c.set, c.report); !ok || got != c.want {
			t.Errorf("%s: %+v, want %+v", c.set, got, c.want)
		}
	}
	// "25 of 25 layers" read as a perfect score once.
	if got, ok := ScoreOf("files", gpu+"The run stopped.\n"); ok {
		t.Errorf("a report with no score read as %+v", got)
	}
}

func TestSecondsPerQuestion(t *testing.T) {
	if got := SecondsPerQuestion("38 of 39 right (97%), 5.3 s and 1176 tokens a question on average."); got != 5.3 {
		t.Errorf("%v", got)
	}
	if got := SecondsPerQuestion("nothing"); got != 0 {
		t.Errorf("%v", got)
	}
}

func TestTestedOn(t *testing.T) {
	for line, want := range map[string]Tested{
		fmt.Sprintf(onCard, "gemma-2-9b-q4"): {"gemma-2-9b-q4", "AMD RX 7900 XTX, 24 GB"},
		"Tested Toskar 0.1.0-dev (unknown) on Apple M5 Pro, with lfm3; the model ran on the GPU (Metal, 25 of 25 layers).\n": {"lfm3", "Apple M5 Pro"},
		"Tested Toskar 1 (a) on Some CPU (8 GB), with m; the model ran on the CPU.\n":                                        {"m", "Some CPU, 8 GB"},
		"Tested Toskar 1 (a) on Some Card, with m.\n":                                                                        {"m", "Some Card"},
		fmt.Sprintf(onCard, "the recommended models"):                                                                        {"the recommended models", "AMD RX 7900 XTX, 24 GB"},
	} {
		if got, ok := TestedOn(line); !ok || got != want {
			t.Errorf("%q: %+v, want %+v", line, got, want)
		}
	}
	if _, ok := TestedOn("The run stopped.\n"); ok {
		t.Error("a report that names nothing was read")
	}
}

func TestScore(t *testing.T) {
	all := map[string]Passed{"quality": {60, 64}, "topics": {20, 22}, "files": {20, 21}, "reasoning": {32, 39}}
	if got, ok := Score(all); !ok || got != 90 {
		t.Errorf("%d %v, want 90", got, ok)
	}
	// Speed and other sets stay out of it.
	withTriage := map[string]Passed{"triage": {0, 30}}
	for k, v := range all {
		withTriage[k] = v
	}
	if got, ok := Score(withTriage); !ok || got != 90 {
		t.Errorf("with triage %d %v, want 90", got, ok)
	}
	delete(all, "files")
	if got, ok := Score(all); ok {
		t.Errorf("a row missing a set scored %d", got)
	}
	if got, ok := Score(map[string]Passed{"quality": {0, 0}, "topics": {1, 1}, "files": {1, 1}, "reasoning": {1, 1}}); ok {
		t.Errorf("a set of no cases scored %d", got)
	}
	if got := Describe(95, 1); got != "95 (v1)" {
		t.Error(got)
	}
}

func TestPublishAddsWeeklyRows(t *testing.T) {
	dir := t.TempDir()
	write := func(folder, name, text string) {
		t.Helper()
		folder = filepath.Join(dir, reportFolderPrefix+folder)
		if err := os.MkdirAll(folder, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(folder, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gemma := fmt.Sprintf(onCard, "gemma-2-9b-q4")
	write("gemma-2-9b-q4", "report.md", gemma+"60 of 64 cases passed (94%, 95% interval 85–98%).\n")
	write("gemma-2-9b-q4", "topics.md", gemma+"20 of 22 cases passed (91%).\n")
	write("gemma-2-9b-q4", "files.md", gemma+"20 of 21 cases passed (95%).\n")
	write("gemma-2-9b-q4", "media.md", gemma+"22 of 23 cases passed.\n")
	write("gemma-2-9b-q4", "reasoning.md", gemma+"32 of 39 right (82%), 1.0 s and 388 tokens a question on average.\n")
	write("gemma-2-9b-q4", "triage.md", gemma+"type right on 26 of 30, precision 62%.\n")
	write("gemma-2-9b-q4", PerformanceFile, `{"model":"gemma-2-9b-q4","os":"linux","runs":"partial","prompt_tokens":4000,"prompt_tokens_per_second":900.5,"tokens_per_second":50.5,"first_token_ms":4400,"effective_tokens_per_second":21.5,"gpu_layers":43,"gpu_layers_total":43,"quantization":"Q4_K_M"}`)
	// Reports for this model, and figures for another, are no row.
	mixed := fmt.Sprintf(onCard, "mixed-model")
	write("mixed-model", "report.md", mixed+"60 of 64 cases passed.\n")
	write("mixed-model", "topics.md", mixed+"20 of 22 cases passed.\n")
	write("mixed-model", "files.md", mixed+"20 of 21 cases passed.\n")
	write("mixed-model", "reasoning.md", mixed+"32 of 39 right (82%), 1.0 s and 388 tokens a question on average.\n")
	write("mixed-model", PerformanceFile, `{"model":"another","runs":"full","prompt_tokens":4000,"prompt_tokens_per_second":1,"tokens_per_second":1,"first_token_ms":1,"effective_tokens_per_second":1}`)
	// A model that doesn't run is a result, with no scores, even with reports beside it.
	write("too-big", "report.md", fmt.Sprintf(onCard, "too-big")+"0 of 64 cases passed.\n")
	write("too-big", PerformanceFile, `{"model":"too-big","runs":"no","why":"out_of_memory","hardware":"AMD Radeon RX 7900 XT/7900 XTX/7900 GRE/7900M (24 GB)"}`)
	// A row missing a set stays out, and so do sets a Score doesn't count.
	partial := fmt.Sprintf(onCard, "partial-model")
	write("partial-model", "report.md", partial+"60 of 64 cases passed.\n")
	write("partial-model", "topics.md", partial+"20 of 22 cases passed.\n")
	write("recommended", "report.md", fmt.Sprintf(onCard, "the recommended models")+"60 of 64 cases passed.\n")
	// The fallback that ran a smaller model under another's name was a real bug (#1033).
	write("qwen2.5-14b-q4", "report.md", fmt.Sprintf(onCard, "qwen2.5-coder-7b-q4")+"60 of 64 cases passed.\n")
	write("qwen2.5-14b-q4", PerformanceFile, `{"model":"qwen2.5-14b-q4","runs":"full","prompt_tokens":1,"prompt_tokens_per_second":1,"tokens_per_second":1,"first_token_ms":1,"effective_tokens_per_second":1}`)
	write("qwen2.5-32b-q4", "report.md", "The run stopped before any case.\n")

	results := filepath.Join(t.TempDir(), "results.json")
	before := `{
  "schema": 1,
  "suites": {"quality": "q", "topics": "t", "files": "f", "reasoning": "r"},
  "results": [
    {"model": "old", "name": "Old", "date": "2026-10-31", "hardware": "x", "method_version": 0, "scores": {"files": {"passed": 1, "of": 1}}}
  ]
}
`
	if err := os.WriteFile(results, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	const run = "https://github.com/yeixio/toskar-core/actions/runs/1"
	names := map[string]string{"gemma-2-9b-q4": "Gemma 2 9B"}
	now := time.Date(2026, 11, 2, 23, 0, 0, 0, time.UTC)
	var said []string
	publish := func() {
		t.Helper()
		if err := Publish(dir, results, 1, names, run, now, func(s string) { said = append(said, s) }); err != nil {
			t.Fatal(err)
		}
	}
	publish()
	if out := strings.Join(said, "\n"); !strings.Contains(out, "qwen2.5-coder-7b-q4") || !strings.Contains(out, "No row for recommended") || !strings.Contains(out, "No row for partial-model") || !strings.Contains(out, "No row for mixed-model") {
		t.Errorf("what it skipped isn't said:\n%s", out)
	}
	raw, err := os.ReadFile(results)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), strings.TrimSuffix(before, "\n  ]\n}\n")) {
		t.Errorf("rewrote what was there:\n%s", raw)
	}
	if !strings.Contains(string(raw), `"hardware": "AMD RX 7900 XTX, 24 GB", "method_version": 1`) {
		t.Errorf("not in the file's style:\n%s", raw)
	}
	var got struct {
		Results []Row `json:"results"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("%v\n%s", err, raw)
	}
	if len(got.Results) != 3 {
		t.Fatalf("%d rows:\n%s", len(got.Results), raw)
	}
	row := got.Results[1]
	no := got.Results[2]
	if no.Model != "too-big" || no.Runs != RunsNo || no.Why != "out_of_memory" || no.Hardware != "AMD RX 7900 XTX, 24 GB" || len(no.Scores) != 0 || no.Performance != nil {
		t.Errorf("a model that doesn't run: %+v", no)
	}
	if row.OS != "Linux" {
		t.Errorf("os %q", row.OS)
	}
	if row.Runs != RunsPartial {
		t.Errorf("runs %q", row.Runs)
	}
	if row.Model != "gemma-2-9b-q4" || row.Name != "Gemma 2 9B" || row.Date != "2026-11-02" || row.MethodVersion != 1 || row.Source != run || row.SecondsPerQuestion != 1.0 {
		t.Errorf("%+v", row)
	}
	if p := row.Performance; p == nil || p.PromptTokensPerSecond != 900.5 || p.TokensPerSecond != 50.5 || p.FirstTokenMs != 4400 || p.EffectiveTokensPerSecond != 21.5 || p.GPULayers != 43 || p.Quantization != "Q4_K_M" {
		t.Errorf("performance %+v", row.Performance)
	}
	want := map[string]Passed{"quality": {60, 64}, "topics": {20, 22}, "files": {20, 21}, "reasoning": {32, 39}}
	if len(row.Scores) != len(want) {
		t.Errorf("scores %+v, want only the sets a Score counts", row.Scores)
	}
	for set, w := range want {
		if row.Scores[set] != w {
			t.Errorf("%s: %+v, want %+v", set, row.Scores[set], w)
		}
	}

	// A job run again adds nothing.
	publish()
	again, err := os.ReadFile(results)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(raw) {
		t.Errorf("a second run changed the file:\n%s", again)
	}
}

func TestRowLineLeavesStringsAlone(t *testing.T) {
	line, err := RowLine(Row{Model: `a:b,"c"`, Name: "<n>", Date: "d", Hardware: "h", Scores: map[string]Passed{"files": {1, 2}}})
	if err != nil {
		t.Fatal(err)
	}
	var back Row
	if err := json.Unmarshal([]byte(line), &back); err != nil {
		t.Fatal(err)
	}
	if back.Model != `a:b,"c"` || back.Name != "<n>" || !strings.Contains(line, `"files": {"passed": 1, "of": 2}`) {
		t.Errorf("%s", line)
	}
}

func TestReadPerformance(t *testing.T) {
	path := filepath.Join(t.TempDir(), PerformanceFile)
	for name, c := range map[string]struct {
		body string
		ok   bool
	}{
		"complete":  {`{"model":"m","runs":"full","prompt_tokens_per_second":1,"tokens_per_second":1,"first_token_ms":1,"effective_tokens_per_second":1}`, true},
		"no runs":   {`{"model":"m","prompt_tokens_per_second":1,"tokens_per_second":1,"first_token_ms":1,"effective_tokens_per_second":1}`, false},
		"won't run": {`{"model":"m","runs":"no","why":"out_of_memory"}`, true},
		"no why":    {`{"model":"m","runs":"no"}`, false},
		"zero":      {`{"model":"m","prompt_tokens_per_second":1,"tokens_per_second":1,"first_token_ms":0,"effective_tokens_per_second":1}`, false},
		"other":     {`{"model":"x","prompt_tokens_per_second":1,"tokens_per_second":1,"first_token_ms":1,"effective_tokens_per_second":1}`, false},
		"not JSON":  {`nope`, false},
	} {
		if err := os.WriteFile(path, []byte(c.body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadPerformance(path, "m"); (err == nil) != c.ok {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := ReadPerformance(path+".missing", "m"); err == nil {
		t.Error("a missing report was read")
	}
}

func TestNextModels(t *testing.T) {
	list := []ListedModel{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}
	const run = "https://github.com/yeixio/toskar-core/actions/runs/1"
	results := []Row{
		{Model: "a", MethodVersion: 1, Date: "2026-11-02", Source: run},
		{Model: "c", MethodVersion: 1, Date: "2026-10-26", Source: run},
		// These don't count: an older version, and a hand-run result.
		{Model: "b", MethodVersion: 0, Date: "2026-11-02", Source: run},
		{Model: "d", MethodVersion: 1, Date: "2026-11-02", Source: "https://github.com/yeixio/toskar-core/pull/1"},
	}
	if got := strings.Join(NextModels(list, results, 1, 3), " "); got != "b d c" {
		t.Errorf("%q: the unmeasured first, in the list's order, then the oldest", got)
	}
	if got := NextModels(list, results, 1, 10); len(got) != 4 {
		t.Errorf("%v", got)
	}
}

func TestPopularAndUnmeasured(t *testing.T) {
	const gb = 1 << 30
	list := []ListedModel{
		{ID: "big", SizeBytes: 20 * gb, Downloads: 900},
		{ID: "small-quiet", SizeBytes: gb, Downloads: 10},
		{ID: "small-popular", SizeBytes: 2 * gb, Downloads: 500},
		{ID: "small-new", SizeBytes: 3 * gb, Downloads: 200},
	}
	if m, ok := Popular(list, 8*gb); !ok || m.ID != "small-popular" {
		t.Errorf("%+v %v: want the most downloaded that fits", m, ok)
	}
	if m, ok := Popular(list, 26*gb); !ok || m.ID != "big" {
		t.Errorf("%+v %v", m, ok)
	}
	// On any computer: a model with a row elsewhere has data.
	results := []Row{{Model: "small-popular", Hardware: "Theirs"}}
	if m, ok := Unmeasured(list, results, 8*gb); !ok || m.ID != "small-new" {
		t.Errorf("%+v %v: want the most downloaded with no result anywhere", m, ok)
	}
	all := []Row{{Model: "small-popular"}, {Model: "small-new"}, {Model: "small-quiet"}}
	if m, ok := Unmeasured(list, all, 8*gb); ok {
		t.Errorf("%+v with everything measured", m)
	}
	if _, ok := Popular(list, gb); ok {
		t.Error("suggested a model that doesn't fit")
	}
	if got := RoomFor(24*gb, 32*gb); got != 24*gb {
		t.Errorf("a card's own memory: %d", got/gb)
	}
	if got := RoomFor(0, 30*gb); got != 18*gb {
		t.Errorf("shared memory: %d", got/gb)
	}
}

func TestToskarScore(t *testing.T) {
	if got, ok := ToskarScore(ReferenceModel, &Performance{TokensPerSecond: 149.1, PromptTokensPerSecond: 4390.8}); !ok || got != 2034 {
		t.Errorf("%d %v, want what toskar.ai shows", got, ok)
	}
	if _, ok := ToskarScore("other", &Performance{TokensPerSecond: 1, PromptTokensPerSecond: 1}); ok {
		t.Error("scored another model")
	}
	if _, ok := ToskarScore(ReferenceModel, nil); ok {
		t.Error("scored nothing")
	}
}

func TestModelLink(t *testing.T) {
	for link, ok := range map[string]bool{
		"https://huggingface.co/a/b/resolve/main/c.gguf":         true,
		"https://huggingface.co/a/b/resolve/main/sub/c.gguf?x=1": true,
		"https://huggingface.co/a/b/blob/main/c.gguf":            false,
		"http://huggingface.co/a/b/resolve/main/c.gguf":          false,
		"https://example.com/a/b/resolve/main/c.gguf":            false,
		"https://huggingface.co/a/b/resolve/main/c.safetensors":  false,
		"qwen3.5-4b-q4": false,
	} {
		if ModelLink(link) != ok {
			t.Errorf("%q: %v", link, !ok)
		}
	}
}

func TestCreditName(t *testing.T) {
	for name, ok := range map[string]bool{"octo-cat": true, "a": true, "bad name": false, "-x": false, "x-": false, "<b>": false, "": false, strings.Repeat("a", 40): false} {
		if ValidCredit(name) != ok {
			t.Errorf("%q: %v", name, !ok)
		}
	}
}
