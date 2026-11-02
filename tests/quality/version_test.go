package quality

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/config"
)

// qualityVersion is version.json: the version of how quality is measured, and
// what the sets held at that version.
type qualityVersion struct {
	Version int               `json:"version"`
	Sets    map[string]string `json:"sets"`
}

func readQualityVersion(t *testing.T) qualityVersion {
	t.Helper()
	raw, err := os.ReadFile("version.json")
	if err != nil {
		t.Fatal(err)
	}
	var v qualityVersion
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// TestVersionCoversTheSets fails when a set changes without its version doing
// so: results are only comparable within one version. Add 1 to version, say what
// changed in docs/quality.md, and rewrite the hashes with
// TOSKAR_UPDATE_QUALITY_HASHES=1 go test ./tests/quality -run TestVersionCoversTheSets.
// Changing how a set is checked, in code, needs the same by hand: no test can see it.
func TestVersionCoversTheSets(t *testing.T) {
	v := readQualityVersion(t)
	if v.Version < 1 || len(v.Sets) == 0 {
		t.Fatalf("version.json: %+v", v)
	}
	update := config.Env("UPDATE_QUALITY_HASHES") != ""
	for name, want := range v.Sets {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		got := hex.EncodeToString(sum[:])
		if update {
			v.Sets[name] = got
		} else if got != want {
			t.Errorf("%s changed without a new quality version: add 1 to version in version.json, say what changed in docs/quality.md, and rewrite the hashes (see this test)", name)
		}
	}
	if update {
		out, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile("version.json", append(out, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// The score a person submits comes from each report's own sentence, and not
// the first numbers in it: "25 of 25 layers" read as a perfect score once.
func TestScoreScriptReadsReports(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not installed")
	}
	dir := t.TempDir()
	const gpu = "Tested Toskar on Apple M5 Pro, with m; the model ran on the GPU (Metal, 25 of 25 layers).\n\n"
	for name, text := range map[string]string{
		"quality.md":   gpu + "57 of 64 cases passed (89%, 95% interval 79–95%); at least 90% must pass.\n",
		"topics.md":    gpu + "The topic controls held 13 of 13 off-topic cases (100%); wrongly refused 0 of 9 on-topic ones (0%); no model\n\n20 of 22 cases passed (91%, 95% interval 70–98%).\n",
		"files.md":     gpu + "20 of 21 cases passed (95%, 95% interval 77–99%).\n",
		"reasoning.md": gpu + "38 of 39 right (97%), 5.3 s and 1176 tokens a question on average.\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	hardware := `{"hostname":"secret-host","cpu":{"model":"Apple M5 Pro"},"memory":{"total_bytes":25769803776},"accelerators":[{"name":"Apple M5 Pro"}]}`
	if out, err := exec.Command(python, "../../scripts/quality_result.py", "m", dir, "version.json", hardware).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Hardware      string  `json:"hardware"`
		MethodVersion int     `json:"method_version"`
		Seconds       float64 `json:"seconds_per_question"`
		Scores        map[string]struct {
			Passed int `json:"passed"`
			Of     int `json:"of"`
		} `json:"scores"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for suite, want := range map[string][2]int{"quality": {57, 64}, "topics": {20, 22}, "files": {20, 21}, "reasoning": {38, 39}} {
		if s := got.Scores[suite]; s.Passed != want[0] || s.Of != want[1] {
			t.Errorf("%s: %+v, want %v", suite, s, want)
		}
	}
	if got.Hardware != "Apple M5 Pro, 24 GB" || got.MethodVersion != readQualityVersion(t).Version || got.Seconds != 5.3 {
		t.Errorf("%+v", got)
	}
	if strings.Contains(string(raw), "secret-host") {
		t.Errorf("result.json names the computer: %s", raw)
	}
}

// The weekly run's rows come from the reports it uploads, in the words they use,
// and a model that wasn't the one asked for never gets a row: the fallback that
// ran a smaller model under another's name was a real bug (#1033).
func TestPublishScriptAddsWeeklyRows(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not installed")
	}
	const onCard = "Tested Toskar 1 (abc) on AMD Radeon RX 7900 XT/7900 XTX/7900 GRE/7900M (24 GB), with %s; the model ran on the GPU (Vulkan, 43 of 43 layers).\n\n"
	dir := t.TempDir()
	write := func(folder, name, text string) {
		t.Helper()
		folder = filepath.Join(dir, "quality-report-real-models-"+folder)
		if err := os.MkdirAll(folder, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(folder, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gemma := fmt.Sprintf(onCard, "gemma-2-9b-q4")
	write("gemma-2-9b-q4", "report.md", gemma+"60 of 64 cases passed (94%, 95% interval 85–98%); at least 90% must pass.\n\n**Covered 64 of 77 cases**, so this isn't a full run.\n")
	write("gemma-2-9b-q4", "topics.md", gemma+"The topic controls held 13 of 13 off-topic cases (100%); wrongly refused 0 of 9 on-topic ones (0%).\n\n20 of 22 cases passed (91%, 95% interval 70–98%).\n")
	write("gemma-2-9b-q4", "files.md", gemma+"20 of 21 cases passed (95%, 95% interval 77–99%).\n")
	write("gemma-2-9b-q4", "media.md", gemma+"22 of 23 cases passed.\n")
	write("gemma-2-9b-q4", "reasoning.md", gemma+"32 of 39 right (82%), 1.0 s and 388 tokens a question on average.\n")
	write("gemma-2-9b-q4", "triage.md", gemma+"type right on 26 of 30, precision 62%, recall 67%, 1.5 s an issue (30 answered of 30).\n")
	write("recommended", "report.md", fmt.Sprintf(onCard, "the recommended models")+"60 of 64 cases passed.\n")
	write("qwen2.5-14b-q4", "report.md", fmt.Sprintf(onCard, "qwen2.5-coder-7b-q4")+"60 of 64 cases passed.\n")
	write("qwen2.5-32b-q4", "report.md", "The run stopped before any case.\n")

	results := filepath.Join(t.TempDir(), "results.json")
	before := `{
  "schema": 1,
  "suites": {"quality": "q", "topics": "t", "files": "f", "reasoning": "r", "triage": "i"},
  "results": [
    {"model": "old", "name": "Old", "date": "2026-10-31", "hardware": "x", "method_version": 0, "scores": {"files": {"passed": 1, "of": 1}}}
  ]
}
`
	if err := os.WriteFile(results, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	const run = "https://github.com/yeixio/toskar-core/actions/runs/1"
	publish := func() string {
		t.Helper()
		out, err := exec.Command(python, "../../scripts/quality_publish.py", dir, results, "version.json", "../../manifests/models/catalog.json", run).CombinedOutput()
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		return string(out)
	}
	if out := publish(); !strings.Contains(out, "qwen2.5-coder-7b-q4") {
		t.Errorf("the wrong model isn't called out:\n%s", out)
	}
	raw, err := os.ReadFile(results)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), strings.TrimSuffix(before, "\n  ]\n}\n")) {
		t.Errorf("rewrote what was there:\n%s", raw)
	}
	var got struct {
		Results []struct {
			Model         string  `json:"model"`
			Name          string  `json:"name"`
			Hardware      string  `json:"hardware"`
			MethodVersion int     `json:"method_version"`
			Source        string  `json:"source"`
			Seconds       float64 `json:"seconds_per_question"`
			Scores        map[string]struct {
				Passed int `json:"passed"`
				Of     int `json:"of"`
			} `json:"scores"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("%v\n%s", err, raw)
	}
	if len(got.Results) != 2 {
		t.Fatalf("%d rows:\n%s", len(got.Results), raw)
	}
	row := got.Results[1]
	if row.Model != "gemma-2-9b-q4" || row.Name != "Gemma 2 9B" || row.Hardware != "AMD RX 7900 XTX, Vulkan" ||
		row.MethodVersion != readQualityVersion(t).Version || row.Source != run || row.Seconds != 1.0 {
		t.Errorf("%+v", row)
	}
	want := map[string][2]int{"quality": {60, 64}, "topics": {20, 22}, "files": {20, 21}, "reasoning": {32, 39}, "triage": {26, 30}}
	if len(row.Scores) != len(want) {
		t.Errorf("scores %+v, want only the sets the file lists", row.Scores)
	}
	for suite, w := range want {
		if s := row.Scores[suite]; s.Passed != w[0] || s.Of != w[1] {
			t.Errorf("%s: %+v, want %v", suite, s, w)
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
