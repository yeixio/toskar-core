package quality

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
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
