package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/imagegen"
	"github.com/yeixio/toskar-core/internal/models"
	"github.com/yeixio/toskar-core/internal/pyenv"
	"github.com/yeixio/toskar-core/internal/speech"
)

// One-step setup, end to end (#1397): on an empty computer, each feature's
// one Set up installs everything it needs, and the feature then says it is
// ready. Downloads come from a stand-in server, so nothing here uses the
// internet: models, Kitten's voices, and the picture and clip programs.
func TestEachFeatureSetsUpFromAnEmptyComputer(t *testing.T) {
	for _, tc := range []struct {
		feature string
		// python is a feature whose setup runs Kitten's installer.
		python, camera, media bool
	}{
		{feature: "voice", python: true},
		{feature: "door", python: true, camera: true},
		{feature: "pictures", media: true},
		{feature: "clips", media: true},
		{feature: "see"},
		{feature: "meaning"},
		{feature: "automationTools"},
	} {
		t.Run(tc.feature, func(t *testing.T) {
			if tc.python {
				if _, err := exec.LookPath("python3"); err != nil {
					t.Skip("python3 is not installed")
				}
			}
			if tc.media && runtime.GOOS == "windows" {
				t.Skip("the stand-in sd-cli is a shell script")
			}
			ctx := context.Background()
			a := emptyComputer(t)
			if tc.camera {
				connectCamera(t, a)
			}
			before := featureByID(t, a, tc.feature)
			if before.Ready {
				t.Fatalf("%s is ready on an empty computer: %+v", tc.feature, before)
			}
			if _, err := a.SetUpFeature(ctx, tc.feature); err != nil {
				t.Fatalf("set up %s: %v", tc.feature, err)
			}
			waitFeatureReady(t, a, tc.feature)
			// Once ready, setting it up again installs nothing.
			if f, err := a.SetUpFeature(ctx, tc.feature); err != nil || !f.Ready || f.SetupBytes != 0 {
				t.Fatalf("set up %s again: %+v, %v", tc.feature, f, err)
			}
		})
	}

	// Training needs a computer that can train; there is nothing to
	// download for it, so setting it up changes nothing and says so by
	// still listing the need with nothing to install.
	t.Run("train", func(t *testing.T) {
		a := emptyComputer(t)
		f, err := a.SetUpFeature(context.Background(), "train")
		if err != nil {
			t.Fatalf("set up train: %v", err)
		}
		if f.Ready || f.SetupBytes != 0 || len(f.Needs) != 1 || f.Needs[0].Install != "" {
			t.Fatalf("train on a computer that can't train: %+v", f)
		}
	})
}

// emptyComputer is a new Toskar with no models, whose downloads all come
// from a stand-in server.
func emptyComputer(t *testing.T) *App {
	t.Helper()
	t.Setenv("TOSKAR_STUB_INFERENCE", "")
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	t.Setenv("TOSKAR_DOWNLOAD_MIRROR", "")
	a, err := New(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })

	files := map[string][]byte{}
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		data, ok := files[strings.TrimPrefix(r.URL.Path, "/")]
		mu.Unlock()
		if !ok {
			// A model file: any small file, since no checksum is asked.
			data = []byte("GGUF stand-in for " + r.URL.Path)
		}
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
	}))
	t.Cleanup(srv.Close)

	// Every catalog model downloads a small file from the server.
	catalog := a.Models.Catalog()
	for _, e := range catalog.List() {
		e.Source.URL, e.Source.SHA256, e.SizeBytes = srv.URL+"/models/"+e.ID, "", 4096
		if e.Projector != nil {
			e.Projector = &models.ModelFile{URL: srv.URL + "/projectors/" + e.ID, SizeBytes: 1024}
		}
		if e.Draft != nil {
			e.Draft = &models.ModelFile{URL: srv.URL + "/drafts/" + e.ID, SizeBytes: 1024}
		}
		catalog.Upsert(e)
	}

	// Kitten installs into a stand-in Python whose worker writes its
	// voices.
	if py, err := exec.LookPath("python3"); err == nil {
		dir := t.TempDir()
		w := &speech.Worker{Engine: &speech.Engine{Python: &setupEnv{py: py, have: map[string]bool{}}, Dir: dir},
			Script: []byte(standInSpeechWorker), Plain: true}
		t.Cleanup(w.Close)
		a.voiceWorker = w
	}

	// Pictures and clips set up a stand-in stable-diffusion.cpp and one
	// small model each.
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	h := &zip.FileHeader{Name: "bin/sd-cli", Method: zip.Deflate}
	h.SetMode(0o755)
	cli, _ := zw.CreateHeader(h)
	_, _ = cli.Write([]byte("#!/bin/sh\nexit 0\n"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	files["sd.zip"] = zipBuf.Bytes()
	mu.Unlock()
	archive := imagegen.Archive{Name: "sd.zip", Size: int64(zipBuf.Len()), SHA256: sha(zipBuf.Bytes())}
	small := func(m imagegen.Model) imagegen.Model {
		m.MemoryBytes, m.GraphicsBytes, m.Stages, m.Untested, m.NeedsLicense = 0, 0, nil, false, false
		m.Files = append([]imagegen.File(nil), m.Files...)
		for i, f := range m.Files {
			data := []byte("weights for " + f.Path)
			mu.Lock()
			files[f.Path] = data
			mu.Unlock()
			m.Files[i].Size, m.Files[i].SHA256 = int64(len(data)), sha(data)
		}
		return m
	}
	for _, st := range []struct {
		setup *imagegen.Setup
		model imagegen.Model
	}{{a.Images, small(imagegen.Catalog()[0])}, {a.Video, small(imagegen.VideoCatalog()[0])}} {
		if st.setup == nil {
			continue
		}
		m := st.model
		st.setup.Catalog = func() []imagegen.Model { return []imagegen.Model{m} }
		st.setup.DownloadFrom(archive, func(f imagegen.File) string { return srv.URL + "/" + f.Path })
	}
	a.capCache.Delete("snapshot")
	return a
}

func featureByID(t *testing.T, a *App, id string) Feature {
	t.Helper()
	for _, f := range a.Features(context.Background()) {
		if f.ID == id {
			return f
		}
	}
	t.Fatalf("no feature %q", id)
	return Feature{}
}

// waitFeatureReady waits for a feature's downloads to finish and the
// feature to say it is ready.
func waitFeatureReady(t *testing.T, a *App, id string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		a.capCache.Delete("snapshot")
		f := featureByID(t, a, id)
		if f.Ready {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s isn't ready after setup: %+v", id, f)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// setupEnv installs Python environments by name, as pyenv does, all with
// this computer's python3.
type setupEnv struct {
	py   string
	mu   sync.Mutex
	have map[string]bool
}

func (e *setupEnv) Ensure(_ context.Context, spec pyenv.Spec, _ pyenv.Progress) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.have[spec.Name] = true
	return e.py, nil
}
func (e *setupEnv) Env() []string                 { return nil }
func (e *setupEnv) Unavailable(pyenv.Spec) string { return "" }
func (e *setupEnv) Status(spec pyenv.Spec) pyenv.Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	return pyenv.Status{Name: spec.Name, Installed: e.have[spec.Name], Python: e.py}
}
func (e *setupEnv) Remove(name string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.have, name)
	return nil
}

// standInSpeechWorker answers as worker.py does: fetch writes Kitten's
// model config, and anything else answers with a little sound.
const standInSpeechWorker = `import base64, json, os, sys
for line in sys.stdin:
    req = json.loads(line)
    if req["op"] == "fetch":
        cfg = {"languages": {"en": {}}, "voices": {"en": [{"key": "eleanor", "name": "Eleanor"}]}}
        with open(os.path.join(req["dir"], "config.json"), "w") as f:
            json.dump(cfg, f)
        out = {}
    else:
        out = {"pcm": base64.b64encode(b"sound").decode(), "rate": 24000, "text": ""}
    out["id"] = req["id"]
    print(json.dumps(out), flush=True)
`
