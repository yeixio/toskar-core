package models

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/yeixio/toskar-core/internal/dlmirror"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Two Toskars sharing one models folder, as CI jobs on one self-hosted
// runner do (#1395): the other one renames the partial file into place
// while this one is still downloading it.
func TestDownloadWhenAnotherToskarFinishesFirst(t *testing.T) {
	payload := []byte("gguf-model-bytes-shared-by-two-toskars")
	sum := sha256.Sum256(payload)
	sha := hex.EncodeToString(sum[:])

	cases := []struct {
		name string
		// body is what this Toskar receives; the file put in place is
		// always the real one.
		body []byte
		sha  string
	}{
		{name: "same bytes", body: payload, sha: sha},
		{name: "partial file was overwritten", body: []byte("garbled by two writers"), sha: sha},
		{name: "no checksum to compare", body: payload},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			db, err := store.Open(filepath.Join(dir, "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			storage := NewStorage(db.SQL, filepath.Join(dir, "models"))
			if err := storage.EnsureDirs(); err != nil {
				t.Fatal(err)
			}
			const id = "shared-model"
			t.Setenv(dlmirror.Env, "")
			d := NewDownloader(storage, events.NewBus(8), db.SQL)
			d.client = &http.Client{Transport: otherFinishes{body: c.body, done: func() {
				// The other Toskar puts its copy in place, moving the partial file away.
				_ = os.Remove(storage.TempPath(id))
				if err := os.WriteFile(storage.ModelPath(id), payload, 0o644); err != nil {
					t.Error(err)
				}
			}}}
			entry := CatalogEntry{ID: id, DisplayName: "Shared", SizeBytes: uint64(len(payload)), MemoryNeededBytes: 1000, Source: contracts.ModelSource{
				URL: "https://huggingface.co/owner/repo/resolve/main/model.gguf", SHA256: c.sha,
			}}
			if err := storage.UpsertCatalogEntry(context.Background(), entry); err != nil {
				t.Fatal(err)
			}
			if err := d.Download(context.Background(), entry); err != nil {
				t.Fatalf("Download: %v", err)
			}
			got, err := os.ReadFile(storage.ModelPath(id))
			if err != nil || string(got) != string(payload) {
				t.Fatalf("model file = %q, %v", got, err)
			}
			if installed, _, _ := storage.IsInstalled(context.Background(), id); !installed {
				t.Fatal("not marked installed")
			}
		})
	}
}

// otherFinishes answers with body, and calls done once it's all been read,
// before this Toskar closes and renames its partial file.
type otherFinishes struct {
	body []byte
	done func()
}

func (o otherFinishes) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{}, Request: r, ContentLength: int64(len(o.body)),
		Body: io.NopCloser(&thenDone{r: bytes.NewReader(o.body), done: o.done}),
	}, nil
}

type thenDone struct {
	r    io.Reader
	done func()
	ran  bool
}

func (t *thenDone) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if err == io.EOF && !t.ran {
		t.ran = true
		t.done()
	}
	return n, err
}
