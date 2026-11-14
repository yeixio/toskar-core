package benchmark

import (
	"strings"
	"testing"
)

// A long-context workload fills about three quarters of its window
// (#1066), at about 27.5 tokens a line, and isn't in the list people pick
// from: it needs the model started with a window that size.
func TestLongContextWorkloads(t *testing.T) {
	for _, n := range LongContexts {
		id := LongContextWorkload(n)
		w, ok := WorkloadByID(id)
		if !ok || w.ID != id || len(w.Prompts) != 1 {
			t.Fatalf("%s: %+v, %v", id, w, ok)
		}
		lines := strings.Count(w.Prompts[0].Text, "\nShipment ") + 1
		if tokens := float64(lines) * tokensPerLine; tokens < 0.7*float64(n) || tokens > 0.8*float64(n) {
			t.Errorf("%s: %d lines, about %.0f tokens of %d", id, lines, tokens, n)
		}
		if !IsLongPrompt(id) {
			t.Errorf("%s isn't read anew each run", id)
		}
		for _, listed := range Workloads() {
			if listed.ID == id {
				t.Errorf("%s is in the list people pick from", id)
			}
		}
	}
	if LongContextWorkload(32768) != "long_prompt_32k" {
		t.Errorf("32K: %s", LongContextWorkload(32768))
	}
	if !IsLongPrompt(LongPromptWorkload) || IsLongPrompt("quick") {
		t.Error("IsLongPrompt")
	}
	// The 4,000-token prompt is as it was.
	if w, _ := WorkloadByID(LongPromptWorkload); strings.Count(w.Prompts[0].Text, "Shipment ") != 160 {
		t.Errorf("the long prompt changed")
	}
}
