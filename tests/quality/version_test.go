package quality

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
		"topics.md":    gpu + "The topic controls held 12 of 13 off-topic cases (92%); wrongly refused 1 of 9 on-topic ones (11%); no model\n",
		"files.md":     gpu + "20 of 21 cases passed (95%, 95% interval 77–99%).\n",
		"reasoning.md": gpu + "38 of 39 right (97%), 5.3 s and 1176 tokens a question on average.\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	hardware := `{"hostname":"secret-host","cpu":{"model":"Apple M5 Pro"},"memory":{"total_bytes":25769803776},"accelerators":[{"name":"Apple M5 Pro"}]}`
	if out, err := exec.Command(python, "../../scripts/quality-result.py", "m", dir, "version.json", hardware).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Hardware      string `json:"hardware"`
		MethodVersion int    `json:"method_version"`
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
	if got.Hardware != "Apple M5 Pro, 24 GB" || got.MethodVersion != readQualityVersion(t).Version {
		t.Errorf("%+v", got)
	}
	if strings.Contains(string(raw), "secret-host") {
		t.Errorf("result.json names the computer: %s", raw)
	}
}
