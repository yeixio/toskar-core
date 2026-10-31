package app

import (
	"context"
	"testing"
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
