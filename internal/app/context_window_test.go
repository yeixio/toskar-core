package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/models"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A chat model starts with Auto's window until the person picks a size, up
// to its own window; the choice is kept, listed with each size's fit, and
// back to Auto with 0 (#962).
func TestModelContextChoice(t *testing.T) {
	a, _, _ := pictureApp(t, false)
	ctx := context.Background()
	a.steadyHW.inv = contracts.HardwareInventory{
		Memory:       contracts.MemoryInfo{TotalBytes: 64 << 30},
		Accelerators: []contracts.Accelerator{{Kind: "gpu", DedicatedVRAM: 24 << 30, Backends: []string{"vulkan"}}},
	}
	a.steadyHW.known = true
	st := a.Models.Storage()
	e := models.CatalogEntry{ID: "small", DisplayName: "Small", Parameters: "3B", Variant: "Q4_K_M", SizeBytes: 2 << 30, Context: 65536,
		Capabilities: contracts.ModelCapabilities{ToolCalling: true}}
	a.Models.Catalog().Upsert(e)
	if err := st.UpsertCatalogEntry(ctx, e); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.ModelPath("small"), []byte("gguf"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkInstalled(ctx, "small", st.ModelPath("small"), "", 4); err != nil {
		t.Fatal(err)
	}
	listed := func() contracts.Model {
		list, err := a.listModelsCluster(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range list {
			if m.ID == "small" {
				return m
			}
		}
		t.Fatal("small isn't listed")
		return contracts.Model{}
	}

	m := listed()
	if m.ContextWindow != 32768 || m.ContextChoice != 0 || a.startWindow(ctx, "small") != 32768 {
		t.Fatalf("auto: window %d choice %d", m.ContextWindow, m.ContextChoice)
	}
	var sizes []int
	for _, o := range m.ContextOptions {
		sizes = append(sizes, o.Tokens)
		if o.Fit == "" {
			t.Errorf("%d has no fit", o.Tokens)
		}
	}
	if len(sizes) != 5 || sizes[0] != 4096 || sizes[4] != 65536 {
		t.Fatalf("options %v, want 4K to its own 64K", sizes)
	}

	if err := a.setModelContext(ctx, "small", 65536); err != nil {
		t.Fatal(err)
	}
	if m := listed(); m.ContextWindow != 65536 || m.ContextChoice != 65536 || a.startWindow(ctx, "small") != 65536 {
		t.Fatalf("chosen: window %d choice %d", m.ContextWindow, m.ContextChoice)
	}
	if err := a.setModelContext(ctx, "small", 0); err != nil {
		t.Fatal(err)
	}
	if m := listed(); m.ContextWindow != 32768 || m.ContextChoice != 0 {
		t.Fatalf("back to auto: window %d choice %d", m.ContextWindow, m.ContextChoice)
	}
	for name, c := range map[string]struct {
		id     string
		tokens int
	}{
		"past its own window": {"small", 131072},
		"not a size":          {"small", 10000},
		"not installed":       {"no-such-model", 8192},
	} {
		if err := a.setModelContext(ctx, c.id, c.tokens); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// A request that needs more raises a model to the smallest size that holds
// it and still fits, for a while and then while the model runs with it; a
// stopped model is checked against the window it starts with, not 8K (#962).
func TestContextWindowRaise(t *testing.T) {
	a, _, _ := pictureApp(t, false)
	ctx := context.Background()
	a.steadyHW.inv = contracts.HardwareInventory{
		Memory:       contracts.MemoryInfo{TotalBytes: 64 << 30},
		Accelerators: []contracts.Accelerator{{Kind: "gpu", DedicatedVRAM: 24 << 30, Backends: []string{"vulkan"}}},
	}
	a.steadyHW.known = true
	st := a.Models.Storage()
	e := models.CatalogEntry{ID: "small", DisplayName: "Small", Parameters: "3B", Variant: "Q4_K_M", SizeBytes: 2 << 30, Context: 131072,
		Capabilities: contracts.ModelCapabilities{ToolCalling: true}}
	a.Models.Catalog().Upsert(e)
	if err := st.UpsertCatalogEntry(ctx, e); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.ModelPath("small"), []byte("gguf"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkInstalled(ctx, "small", st.ModelPath("small"), "", 4); err != nil {
		t.Fatal(err)
	}

	if got := a.knownWindow(ctx, "small"); got != 32768 {
		t.Fatalf("a stopped model's window: %d, want Auto's 32768", got)
	}
	if got := a.growWindow(ctx, "small", 20000); got != 0 {
		t.Errorf("Auto already holds 20,000: raised to %d", got)
	}
	if got := a.growWindow(ctx, "small", 50000); got != 65536 {
		t.Fatalf("raise for 50,000: %d, want 65536", got)
	}
	if got := a.startWindow(ctx, "small"); got != 65536 {
		t.Fatalf("starts with %d after the raise", got)
	}
	if got := a.growWindow(ctx, "small", 500000); got != 0 {
		t.Errorf("past its own window: raised to %d", got)
	}
	if got := a.growWindow(ctx, "no-such-model", 50000); got != 0 {
		t.Errorf("a model that isn't here: raised to %d", got)
	}

	// Past the grace, with no model running with it, the raise is over.
	a.raisedWindows.Store("small", raisedWindow{tokens: 65536, at: time.Now().Add(-raiseGrace - time.Second)})
	if got := a.startWindow(ctx, "small"); got != 32768 {
		t.Errorf("after the raise: %d, want Auto's 32768", got)
	}
}
