package models

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/yeixio/toskar-core/internal/gguf/gguftest"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A model that thinks before it answers says so in its capabilities (#840):
// from the catalog, from a file added by hand, and from the file of one
// installed from Hugging Face, whose entry doesn't know. Being tagged for
// reasoning well isn't thinking first.
func TestModelsThink(t *testing.T) {
	m, storage, dir := importManager(t)
	ctx := context.Background()
	qwen3 := "{%- if enable_thinking is defined and enable_thinking is false %}{{- '<think>\\n\\n</think>\\n\\n' }}{%- endif %}"

	added, err := m.ImportFile(ctx, ImportRequest{Path: gguftest.Write(t, filepath.Join(dir, "elsewhere"), "Qwen3-8B-Q4_K_M.gguf", gguftest.Options{Name: "Qwen3 8B", ChatTemplate: qwen3}), InPlace: true})
	if err != nil {
		t.Fatal(err)
	}
	waitInstalled(t, storage, added.ID)

	m.catalog.Upsert(CatalogEntry{ID: "r1", DisplayName: "R1", Capabilities: contracts.ModelCapabilities{Thinks: true}})
	m.catalog.Upsert(CatalogEntry{ID: "reasons", DisplayName: "Reasons", Tags: []string{"general", "reasoning"}})
	// From Hugging Face: the entry is the repository's, the file is gpt-oss.
	hf := CatalogEntry{ID: "hf-oss", DisplayName: "gpt-oss", Tags: []string{"general"}, Dynamic: true}
	m.catalog.Upsert(hf)
	if err := storage.UpsertCatalogEntry(ctx, hf); err != nil {
		t.Fatal(err)
	}
	oss := gguftest.Write(t, filepath.Join(dir, "hf"), "gpt-oss-20b.gguf", gguftest.Options{Name: "gpt-oss-20b", ChatTemplate: "{{- '<|start|>assistant<|channel|>analysis<|message|>' }}"})
	if err := storage.MarkInstalled(ctx, "hf-oss", oss, "", 1); err != nil {
		t.Fatal(err)
	}

	list, err := m.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, model := range list {
		got[model.ID] = model.Capabilities.Thinks
	}
	for id, want := range map[string]bool{added.ID: true, "r1": true, "hf-oss": true, "reasons": false} {
		if got[id] != want {
			t.Errorf("%s: thinks = %v, want %v", id, got[id], want)
		}
		if m.Thinks(ctx, id) != want {
			t.Errorf("Thinks(%s) = %v, want %v", id, !want, want)
		}
	}
	if m.Thinks(ctx, "unknown") {
		t.Error("an unknown model thinks")
	}
}

// A model that answers first thinks only when the turn asks (#593); the
// others think unless the turn says not to (#840).
func TestThinkingOff(t *testing.T) {
	m, _, _ := importManager(t)
	m.catalog.Upsert(CatalogEntry{ID: "first", DisplayName: "First", Thinking: ThinkingWhenAsked})
	m.catalog.Upsert(CatalogEntry{ID: "thinks", DisplayName: "Thinks"})
	cases := []struct {
		model             string
		noThinking, asked bool
		want              bool
	}{
		{"first", false, false, true},
		{"first", false, true, false},
		{"first", true, false, true},
		{"thinks", false, false, false},
		{"thinks", true, false, true},
		{"thinks", false, true, false},
		{"unknown", false, false, false},
	}
	for _, tc := range cases {
		if got := m.ThinkingOff(tc.model, tc.noThinking, tc.asked); got != tc.want {
			t.Errorf("%s no thinking %v asked %v: off = %v, want %v", tc.model, tc.noThinking, tc.asked, got, tc.want)
		}
	}
}

// Every catalog model's thinking is one Core knows.
func TestCatalogThinking(t *testing.T) {
	c, err := NewCatalogEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	for _, err := range thinkingErrors(c.List()) {
		t.Error(err)
	}
	if errs := thinkingErrors([]CatalogEntry{{ID: "x", Thinking: "sometimes"}}); len(errs) != 1 {
		t.Errorf("thinking %q passed", "sometimes")
	}
}
