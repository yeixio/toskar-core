package models

import (
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Auto gives a chat model a bigger window only where it stays an excellent
// fit and in graphics memory (#962), never past the model's own.
func TestAutoContext(t *testing.T) {
	catalog, err := NewCatalogEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	get := func(id string) CatalogEntry {
		e, ok := catalog.Get(id)
		if !ok {
			t.Fatalf("%s isn't in the catalog", id)
		}
		return e
	}
	card := func(ram, vram float64) contracts.HardwareInventory {
		return contracts.HardwareInventory{
			Memory:       contracts.MemoryInfo{TotalBytes: gib(ram)},
			Accelerators: []contracts.Accelerator{{Kind: "gpu", DedicatedVRAM: gib(vram), Backends: []string{"vulkan"}}},
		}
	}
	for _, c := range []struct {
		name  string
		model CatalogEntry
		hw    contracts.HardwareInventory
		want  int
	}{
		{"small model, big Mac", get("qwen2.5-3b-q4"), apple(gib(24), gib(16)), 32768},
		{"7B on a 16 GB Mac", get("qwen2.5-7b-q4"), apple(gib(16), gib(10)), 16384},
		{"14B on a 16 GB Mac stays", get("qwen2.5-14b-q4"), apple(gib(16), gib(10)), 8192},
		{"14B on a 16 GB card", get("qwen2.5-14b-q4"), card(32, 16), 16384},
		{"14B on a 24 GB card", get("qwen2.5-14b-q4"), card(64, 24), 32768},
		{"32B stays on a 24 GB card", get("qwen2.5-32b-q4"), card(64, 24), 8192},
		{"its own window is the most", get("gemma-2-9b-q4"), card(64, 24), 8192},
		{"a smaller own window", CatalogEntry{ID: "x", SizeBytes: gib(1), Parameters: "1B", Variant: "Q4_K_M", Context: 4096, Runtime: []string{"llamacpp"}}, card(64, 24), 4096},
		{"an unknown window", CatalogEntry{ID: "y", SizeBytes: gib(1), Parameters: "1B", Variant: "Q4_K_M", Runtime: []string{"llamacpp"}}, card(64, 24), 8192},
		{"memory not known", get("qwen2.5-3b-q4"), contracts.HardwareInventory{}, 8192},
	} {
		if got := AutoContext(c.model, c.hw); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
}

// The fit is shown at the window the model starts with, so its memory
// counts the bigger cache.
func TestFitUsesTheAutoWindow(t *testing.T) {
	catalog, err := NewCatalogEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	e, _ := catalog.Get("qwen2.5-7b-q4")
	hw := apple(gib(24), gib(16))
	auto := ClassifyFit(e, hw, "", "", FitOptions{})
	at8k := ClassifyFit(e, hw, "", "", FitOptions{ContextTokens: 8192})
	if auto.ContextTokens != 32768 || auto.RuntimeMemoryLowBytes <= at8k.RuntimeMemoryLowBytes || auto.Label != contracts.FitExcellent {
		t.Errorf("auto %d tokens, %d bytes (8K %d), %s", auto.ContextTokens, auto.RuntimeMemoryLowBytes, at8k.RuntimeMemoryLowBytes, auto.Label)
	}
}
