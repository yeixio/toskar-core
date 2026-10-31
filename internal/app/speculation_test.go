package app

import (
	"context"
	"os"
	"testing"

	"github.com/yeixio/toskar-core/internal/models"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Speculation is Auto for an installed chat model until it's turned off,
// and the choice is kept, listed, and sent when the model starts (#835).
func TestModelSpeculationChoice(t *testing.T) {
	a, _, _ := pictureApp(t, false)
	ctx := context.Background()
	choice := func(id string) string {
		list, err := a.listModelsCluster(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range list {
			if m.ID == id {
				return m.Speculation
			}
		}
		return "missing"
	}
	if got := choice("text-model"); got != SpeculationAuto {
		t.Fatalf("default: %q", got)
	}
	if err := a.setModelSpeculation(ctx, "text-model", SpeculationOff); err != nil {
		t.Fatal(err)
	}
	if got := choice("text-model"); got != SpeculationOff || !a.speculationOff(ctx, "text-model") {
		t.Fatalf("off: %q", got)
	}
	if a.speculationOff(ctx, "see-model") {
		t.Fatal("another model turned off too")
	}
	if err := a.setModelSpeculation(ctx, "text-model", SpeculationAuto); err != nil {
		t.Fatal(err)
	}
	if got := choice("text-model"); got != SpeculationAuto {
		t.Fatalf("back to auto: %q", got)
	}
	for name, c := range map[string][2]string{
		"unknown choice": {"text-model", "sometimes"},
		"not installed":  {"no-such-model", SpeculationOff},
	} {
		if err := a.setModelSpeculation(ctx, c[0], c[1]); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// A model starts with its draft when it fits this computer well, and
// without it when it doesn't, or when the draft isn't here (#835).
func TestDraftOnlyWhenItFits(t *testing.T) {
	a, _, _ := pictureApp(t, false)
	ctx := context.Background()
	st := a.Models.Storage()
	add := func(id, params string, size uint64) {
		e := models.CatalogEntry{ID: id, DisplayName: id, Parameters: params, Variant: "Q4_K_M", SizeBytes: size,
			Capabilities: contracts.ModelCapabilities{ToolCalling: true},
			Draft:        &models.ModelFile{URL: "http://unused/draft.gguf", SizeBytes: 1 << 20}}
		a.Models.Catalog().Upsert(e)
		if err := st.UpsertCatalogEntry(ctx, e); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(st.ModelPath(id), []byte("gguf"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := st.MarkInstalled(ctx, id, st.ModelPath(id), "", 4); err != nil {
			t.Fatal(err)
		}
	}
	add("small", "1B", 600<<20)
	add("huge", "2000B", 1200<<30)
	if got := a.draftPath(ctx, "small"); got != "" {
		t.Fatalf("no draft file yet: %q", got)
	}
	for _, id := range []string{"small", "huge"} {
		if err := os.WriteFile(st.DraftPath(id), []byte("draft"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := a.draftPath(ctx, "small"); got != st.DraftPath("small") {
		t.Errorf("fits: %q", got)
	}
	if got := a.draftPath(ctx, "huge"); got != "" {
		t.Errorf("doesn't fit: %q", got)
	}
}
