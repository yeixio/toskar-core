package models

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/yeixio/toskar-core/internal/gguf/gguftest"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

const takesTools = "{%- if tools %}{{- tools | tojson }}{%- endif %}"

// A model installed from an address says what its file's header does: its
// family and window, and that it calls tools. What the request set stays.
func TestInstallFromURLReadsTheHeader(t *testing.T) {
	weights := gguftest.Bytes(gguftest.Options{Name: "Qwen 3.8", Architecture: "qwen3", Context: 32768, ChatTemplate: takesTools})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(weights)
	}))
	defer srv.Close()
	m, _, _ := importManager(t)
	ctx := context.Background()

	id, err := m.InstallFromURL(ctx, contracts.InstallFromURLRequest{SourceURL: srv.URL + "/org/Q-GGUF/resolve/main/q-Q4_K_M.gguf", ID: "q", Parameters: "27B"}, true)
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := m.Catalog().Get(id)
	if !entry.Capabilities.ToolCalling || entry.Family != "qwen3" || entry.Context != 32768 || entry.Parameters != "27B" || !entry.HeaderRead {
		t.Fatalf("entry = %+v", entry)
	}
	models, err := m.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range models {
		if model.ID == id && !model.Capabilities.ToolCalling {
			t.Error("the model list says it can't call tools")
		}
	}
	// Saved, for the next start.
	stored, _ := m.Storage().ListDynamicCatalog(ctx)
	for _, e := range stored {
		if e.ID == id && (!e.Capabilities.ToolCalling || e.Family != "qwen3" || !e.HeaderRead) {
			t.Errorf("stored = %+v", e)
		}
	}
}

// A model installed from an address before Core read its header is read
// once at start, and not again; a curated model isn't second-guessed.
func TestReadHeadersBackfills(t *testing.T) {
	m, storage, dir := importManager(t)
	ctx := context.Background()
	install := func(e CatalogEntry, o gguftest.Options) string {
		t.Helper()
		path := gguftest.Write(t, filepath.Join(dir, "models"), e.ID+".gguf", o)
		m.catalog.Upsert(e)
		if err := storage.UpsertCatalogEntry(ctx, e); err != nil {
			t.Fatal(err)
		}
		if err := storage.MarkInstalled(ctx, e.ID, path, "", 1); err != nil {
			t.Fatal(err)
		}
		return path
	}
	// As stored by a Core from before this read.
	oldPath := install(CatalogEntry{ID: "old-hf", Dynamic: true, Source: contracts.ModelSource{Format: "gguf"}},
		gguftest.Options{Architecture: "qwen3", Context: 8192, ChatTemplate: takesTools})
	install(CatalogEntry{ID: "curated", Family: "llama", Context: 4096},
		gguftest.Options{Architecture: "qwen3", Context: 8192, ChatTemplate: takesTools})

	m.ReadHeaders(ctx)
	entry, _ := m.catalog.Get("old-hf")
	if !entry.Capabilities.ToolCalling || entry.Family != "qwen3" || entry.Context != 8192 || !entry.HeaderRead {
		t.Fatalf("backfilled = %+v", entry)
	}
	if c, _ := m.catalog.Get("curated"); c.Capabilities.ToolCalling || c.Family != "llama" || c.Context != 4096 || c.HeaderRead {
		t.Errorf("curated = %+v", c)
	}

	// On the next start, the stored entry is used and the file isn't read:
	// a header that now says otherwise changes nothing.
	if err := os.WriteFile(oldPath, gguftest.Bytes(gguftest.Options{Architecture: "llama", Context: 2048}), 0o644); err != nil {
		t.Fatal(err)
	}
	next := NewManager(&Catalog{}, storage, m.downloader, m.bus)
	next.SyncDynamic(ctx)
	next.ReadHeaders(ctx)
	if e, _ := next.catalog.Get("old-hf"); !e.Capabilities.ToolCalling || e.Family != "qwen3" || e.Context != 8192 {
		t.Errorf("read again: %+v", e)
	}
}
