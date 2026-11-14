package huginn

import "testing"

// Each level thinks as long as its name says, for a model that takes it
// (#1140); Auto, before it's resolved, leaves the model's own.
func TestEffortReasoning(t *testing.T) {
	for e, want := range map[Effort]string{EffortFast: "low", EffortBalanced: "medium", EffortThorough: "high", EffortAuto: "", "": ""} {
		if got := e.Reasoning(); got != want {
			t.Errorf("%q: %q, want %q", e, got, want)
		}
	}
}
