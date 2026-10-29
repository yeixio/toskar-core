package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/api"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/models"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// toskarctl models against Core's own API, downloader, and events: a model
// is listed, installed with its progress shown, and listed as installed.
// The model downloads from a local server in four pieces, so nothing is
// fetched from the internet.
func TestModelsInstallOnCore(t *testing.T) {
	weights := bytes.Repeat([]byte("gguf"), 1<<18) // 1 MiB
	sum := sha256.Sum256(weights)
	files := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(weights)))
		quarter := len(weights) / 4
		for i := 0; i < 4; i++ {
			_, _ = w.Write(weights[i*quarter : (i+1)*quarter])
			w.(http.Flusher).Flush()
			time.Sleep(300 * time.Millisecond)
		}
	}))
	t.Cleanup(files.Close)

	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	storage := models.NewStorage(db.SQL, filepath.Join(dir, "models"))
	bus := events.NewBus(64)
	catalog := &models.Catalog{}
	catalog.Upsert(models.CatalogEntry{
		ID: "tiny-q4", DisplayName: "Tiny", SizeBytes: uint64(len(weights)),
		Source: contracts.ModelSource{URL: files.URL + "/tiny.gguf", SHA256: hex.EncodeToString(sum[:])},
	})
	manager := models.NewManager(catalog, storage, models.NewDownloader(storage, bus, db.SQL), bus)
	core := httptest.NewServer(api.NewServer(api.Dependencies{
		Bus:        bus,
		ListModels: manager.List,
		InstallModel: func(ctx context.Context, id string, wait bool, _ string) error {
			return manager.Install(ctx, id, wait)
		},
	}).Handler())
	t.Cleanup(core.Close)
	client := daemonClient{base: core.URL, client: core.Client()}

	var out bytes.Buffer
	if err := modelsCommand([]string{"install", "tiny-q4"}, client, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) < 3 || lines[len(lines)-1] != "Installed Tiny." || !strings.HasPrefix(lines[0], "Downloading Tiny: ") || lines[len(lines)-2] != "Downloading Tiny: 100% (1 MB of 1 MB)" {
		t.Errorf("install printed:\n%s", out.String())
	}
	if got, err := os.ReadFile(storage.ModelPath("tiny-q4")); err != nil || !bytes.Equal(got, weights) {
		t.Errorf("model file: %d bytes, %v", len(got), err)
	}

	out.Reset()
	if err := modelsCommand([]string{"list", "--installed"}, client, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "tiny-q4") || !strings.Contains(out.String(), "yes") {
		t.Errorf("list --installed:\n%s", out.String())
	}
	out.Reset()
	if err := modelsCommand([]string{"install", "tiny-q4"}, client, &out); err != nil || out.String() != "Tiny is already installed.\n" {
		t.Errorf("again: %v %q", err, out.String())
	}
}
