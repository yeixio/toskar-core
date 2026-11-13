package app

import (
	"context"
	"os"
	"testing"

	"github.com/yeixio/toskar-core/internal/models"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// slotsApp is an app with one chat model installed, "small", on a computer
// with mem of memory and no graphics card.
func slotsApp(t *testing.T, mem uint64) *App {
	t.Helper()
	a, _, _ := pictureApp(t, false)
	ctx := context.Background()
	a.steadyHW.inv = contracts.HardwareInventory{Memory: contracts.MemoryInfo{TotalBytes: mem}}
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
	return a
}

func codeOf(err error) string {
	code, _ := contracts.ErrorCode(err)
	return code
}

// A model answers one request at a time until the person raises its slots,
// 1 to 4. The setting is kept, listed, started with, and read by swarms
// (#1135).
func TestModelSlotsSetting(t *testing.T) {
	a := slotsApp(t, 64<<30)
	ctx := context.Background()
	local := a.Config.Get().NodeID
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
	if m := listed(); m.Slots != 1 || a.modelSlots(ctx, "small") != 1 || a.swarmSlots(local, "small") != 1 {
		t.Fatalf("default: listed %d", m.Slots)
	}
	if err := a.setModelSlots(ctx, "small", 2); err != nil {
		t.Fatal(err)
	}
	if m := listed(); m.Slots != 2 || a.swarmSlots(local, "small") != 2 || a.swarmSlots("", "small") != 2 {
		t.Fatalf("two: listed %d, swarm %d", m.Slots, a.swarmSlots(local, "small"))
	}
	peers, err := a.listModelsForPeers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range peers {
		if m.ID == "small" && m.Slots != 2 {
			t.Errorf("a paired computer is told %d slots", m.Slots)
		}
	}
	// Each slot keeps a whole window, never below 8K.
	if w := a.startWindow(ctx, "small"); w < slotWindowFloor {
		t.Errorf("each slot starts with %d", w)
	}
	if err := a.setModelSlots(ctx, "small", 1); err != nil {
		t.Fatal(err)
	}
	if m := listed(); m.Slots != 1 {
		t.Fatalf("back to one: %d", m.Slots)
	}

	for name, c := range map[string]struct {
		id    string
		slots int
		code  string
	}{
		"none":          {"small", 0, "MODEL_SLOTS_INVALID"},
		"past four":     {"small", 5, "MODEL_SLOTS_INVALID"},
		"not installed": {"no-such-model", 2, ""},
	} {
		err := a.setModelSlots(ctx, c.id, c.slots)
		if err == nil || codeOf(err) != c.code {
			t.Errorf("%s: %v (code %q), want code %q", name, err, codeOf(err), c.code)
		}
	}
}

// Core refuses slots that leave each less than a task's context, or that
// don't fit the computer's memory with every slot's cache.
func TestModelSlotsRefused(t *testing.T) {
	a := slotsApp(t, 64<<30)
	ctx := context.Background()
	if err := a.setModelContext(ctx, "small", 4096); err != nil {
		t.Fatal(err)
	}
	err := a.setModelSlots(ctx, "small", 2)
	code, params := contracts.ErrorCode(err)
	if code != "MODEL_SLOTS_CONTEXT" || params["context_per_slot"] != 4096 || params["needed"] != slotWindowFloor {
		t.Fatalf("a 4K window with two slots: %v (%s %v)", err, code, params)
	}
	if a.modelSlots(ctx, "small") != 1 {
		t.Fatal("a refused setting was kept")
	}

	// 64K a slot is too much for four on 6 GB, though one fits.
	small := slotsApp(t, 6<<30)
	if err := small.setModelContext(ctx, "small", 65536); err != nil {
		t.Fatal(err)
	}
	if err := small.setModelSlots(ctx, "small", 4); codeOf(err) != "MODEL_SLOTS_MEMORY" {
		t.Fatalf("four 64K caches on 6 GB: %v", err)
	}
	if err := small.setModelSlots(ctx, "small", 1); err != nil {
		t.Fatal(err)
	}
}

// More slots, more memory: each keeps its own cache, and Auto picks a
// window per slot that still fits as well.
func TestSlotsMemoryEstimate(t *testing.T) {
	a := slotsApp(t, 64<<30)
	e, _ := a.Models.Catalog().Get("small")
	hw := a.steadyHW.inv
	one := models.ClassifyFit(e, hw, "", "", models.FitOptions{ContextTokens: 16384})
	two := models.ClassifyFit(e, hw, "", "", models.FitOptions{ContextTokens: 16384, Slots: map[string]int{"small": 2}})
	if two.RuntimeMemoryLowBytes <= one.RuntimeMemoryLowBytes || two.ContextTokens != 16384 {
		t.Fatalf("two slots %d bytes, one %d", two.RuntimeMemoryLowBytes, one.RuntimeMemoryLowBytes)
	}
	if a, b := models.AutoContextSlots(e, hw, 4), models.AutoContext(e, hw); a > b || a < slotWindowFloor {
		t.Fatalf("Auto with four slots %d, one %d", a, b)
	}
}

// A paired computer's model has the slots it last said, and one when it
// said none.
func TestRemoteSlots(t *testing.T) {
	a := slotsApp(t, 64<<30)
	a.noteRemoteSlots("pc-2", []contracts.Model{
		{ID: "qwen", Installed: true, Slots: 3},
		{ID: "old", Installed: true},
	})
	if got := a.swarmSlots("pc-2", "qwen"); got != 3 {
		t.Errorf("qwen on pc-2: %d", got)
	}
	if got := a.swarmSlots("pc-2", "old"); got != 1 {
		t.Errorf("a computer from before slots: %d", got)
	}
	if got := a.swarmSlots("pc-3", "qwen"); got != 1 {
		t.Errorf("unknown: %d", got)
	}
}
