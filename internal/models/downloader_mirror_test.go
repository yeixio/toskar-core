package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/yeixio/toskar-core/internal/dlmirror"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// sourceTransport answers requests to huggingface.co with handler and
// sends everything else, such as the mirror, over the network.
type sourceTransport struct{ handler http.Handler }

func (t sourceTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != "huggingface.co" {
		return http.DefaultTransport.RoundTrip(r)
	}
	rec := httptest.NewRecorder()
	t.handler.ServeHTTP(rec, r)
	return rec.Result(), nil
}

func TestDownloadThroughMirror(t *testing.T) {
	payload := []byte("gguf-model-bytes-for-the-mirror-test")
	sum := sha256.Sum256(payload)
	sha := hex.EncodeToString(sum[:])
	const source = "https://huggingface.co/owner/repo/resolve/main/model.gguf"

	cases := []struct {
		name       string
		mirror     func(w http.ResponseWriter, r *http.Request)
		down       bool
		wantSource bool
	}{
		{name: "mirror has it", mirror: func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/hf/owner/repo/resolve/main/model.gguf" {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(payload)
		}},
		{name: "mirror sends other bytes", wantSource: true, mirror: func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("not the model"))
		}},
		{name: "mirror errors", wantSource: true, mirror: func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "upstream failed", http.StatusBadGateway)
		}},
		{name: "mirror is down", wantSource: true, down: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if c.mirror != nil {
					c.mirror(w, r)
				}
			}))
			if c.down {
				mirror.Close()
			} else {
				defer mirror.Close()
			}
			t.Setenv(dlmirror.Env, mirror.URL)

			var fromSource atomic.Int32
			dir := t.TempDir()
			db, err := store.Open(filepath.Join(dir, "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			storage := NewStorage(db.SQL, filepath.Join(dir, "models"))
			dl := NewDownloader(storage, events.NewBus(8), db.SQL)
			dl.client = &http.Client{Transport: sourceTransport{http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fromSource.Add(1)
				_, _ = w.Write(payload)
			})}}

			entry := CatalogEntry{
				ID: "mirrored", DisplayName: "Mirrored", SizeBytes: uint64(len(payload)), MemoryNeededBytes: 1000,
				Source: contracts.ModelSource{URL: source, SHA256: sha},
			}
			if err := storage.UpsertCatalogEntry(context.Background(), entry); err != nil {
				t.Fatal(err)
			}
			if err := dl.Download(context.Background(), entry); err != nil {
				t.Fatal(err)
			}
			if installed, _, err := storage.IsInstalled(context.Background(), entry.ID); err != nil || !installed {
				t.Fatalf("installed=%v err=%v", installed, err)
			}
			if got := fromSource.Load() > 0; got != c.wantSource {
				t.Errorf("downloaded from the source: %v, want %v", got, c.wantSource)
			}
		})
	}
}
