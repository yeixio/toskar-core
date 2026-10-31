package models

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A model is downloaded with its draft; one installed before it had a draft
// gets just the draft; deleting the model deletes the draft (#835).
func TestModelDownloadsItsDraft(t *testing.T) {
	weights, draft := []byte("weights"), []byte("draft")
	var weightGets, draftGets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/model.gguf":
			weightGets.Add(1)
			_, _ = w.Write(weights)
		case "/draft.gguf":
			draftGets.Add(1)
			_, _ = w.Write(draft)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	storage := NewStorage(db.SQL, filepath.Join(dir, "models"))
	bus := events.NewBus(64)
	catalog := &Catalog{}
	entry := CatalogEntry{
		ID: "big", DisplayName: "Big", SizeBytes: uint64(len(weights) + len(draft)),
		Source: contracts.ModelSource{URL: srv.URL + "/model.gguf", SHA256: sha(weights)},
		Draft:  &ModelFile{URL: srv.URL + "/draft.gguf", SHA256: sha(draft), SizeBytes: uint64(len(draft))},
	}
	catalog.Upsert(entry)
	m := NewManager(catalog, storage, NewDownloader(storage, bus, db.SQL), bus)
	ctx := context.Background()

	if err := m.Install(ctx, "big", true); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(m.DraftPath("big")); string(got) != "draft" {
		t.Fatalf("draft at %q = %q", m.DraftPath("big"), got)
	}

	_ = os.Remove(storage.DraftPath("big"))
	if m.DraftPath("big") != "" {
		t.Fatal("a draft path without the file")
	}
	if err := m.Install(ctx, "big", true); err != nil {
		t.Fatal(err)
	}
	if m.DraftPath("big") == "" || weightGets.Load() != 1 || draftGets.Load() != 2 {
		t.Fatalf("draft=%q weights=%d drafts=%d", m.DraftPath("big"), weightGets.Load(), draftGets.Load())
	}

	if err := m.Delete(ctx, "big"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(storage.DraftPath("big")); !os.IsNotExist(err) {
		t.Fatal("the draft was left behind")
	}
}

// A draft counts toward the memory a model needs.
func TestDraftCountsInFit(t *testing.T) {
	plain := CatalogEntry{ID: "big", Parameters: "14B", Variant: "Q4_K_M", SizeBytes: 9_000_000_000}
	withDraft := plain
	withDraft.Draft = &ModelFile{SizeBytes: 491_400_032}
	low, high := draftBytes(withDraft, 32768)
	if low == 0 || high < low {
		t.Fatalf("draft bytes %d–%d", low, high)
	}
	if l, h := draftBytes(plain, 32768); l != 0 || h != 0 {
		t.Fatalf("no draft: %d–%d", l, h)
	}
}
