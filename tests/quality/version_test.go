package quality

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/pkg/contracts"
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

// A model on the public list that isn't in the catalog installs from its exact file.
func TestInstallModelFromTheList(t *testing.T) {
	var asked contracts.InstallFromURLRequest
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/models/install-from-url":
			_ = json.NewDecoder(r.Body).Decode(&asked)
			_, _ = w.Write([]byte(`{"status":"started"}`))
		case r.URL.Path == "/api/v1/models" && asked.SourceURL != "":
			_ = json.NewEncoder(w).Encode([]contracts.Model{{ID: "listed", Installed: true}})
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	defer daemon.Close()
	list := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"id":"listed","source_url":"https://huggingface.co/a/b/resolve/main/c.gguf"},{"id":"catalog-only"}]}`))
	}))
	defer list.Close()
	t.Setenv("TOSKAR_QUALITY_MODEL_LIST", list.URL)

	newRealDriver(daemon.URL).installModel(t, "listed")
	if asked.SourceURL != "https://huggingface.co/a/b/resolve/main/c.gguf" || asked.ID != "listed" {
		t.Errorf("asked to install %+v", asked)
	}
}
