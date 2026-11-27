package app

import (
	"strings"
	"testing"
)

func gpuIDs(ms []gpuModel) string {
	var out []string
	for _, m := range ms {
		out = append(out, m.ModelID)
	}
	return strings.Join(out, ",")
}

// The idle models holding the graphics card are unloaded, least recently
// used first, until the model starting fits; models that must stay, stay.
// The running models are the ones that put a 27B on the CPU on a 24 GB
// card.
func TestGPUUnloads(t *testing.T) {
	const gb = uint64(1 << 30)
	running := []gpuModel{
		{ModelID: "qwen2.5-14b", GPU: 15 * gb, LastUsed: 1},
		{ModelID: "qwen3-4b", GPU: 9 * gb, LastUsed: 2, Stays: true},
	}
	// The 27B needs more than the card: everything that can go, goes.
	if got := gpuIDs(gpuUnloads(running, 34*gb, 24*gb)); got != "qwen2.5-14b" {
		t.Fatalf("27B unloads %q", got)
	}
	// A small model fits beside both.
	if got := gpuIDs(gpuUnloads(running[1:], 4*gb, 24*gb)); got != "" {
		t.Fatalf("unloaded with room to spare: %q", got)
	}
	// Oldest first, and no more than needed.
	three := []gpuModel{
		{ModelID: "new", GPU: 6 * gb, LastUsed: 30},
		{ModelID: "old", GPU: 6 * gb, LastUsed: 10},
		{ModelID: "mid", GPU: 6 * gb, LastUsed: 20},
	}
	if got := gpuIDs(gpuUnloads(three, 14*gb, 24*gb)); got != "old,mid" {
		t.Fatalf("unloads %q, want old,mid", got)
	}
	// No dedicated card, or a model of unknown size: nothing.
	if got := gpuIDs(gpuUnloads(three, 10*gb, 0)); got != "" {
		t.Fatalf("no card unloads %q", got)
	}
	if got := gpuIDs(gpuUnloads(three, 0, 24*gb)); got != "" {
		t.Fatalf("unknown size unloads %q", got)
	}
	// Models on the CPU free nothing on the card.
	cpu := []gpuModel{{ModelID: "cpu", LastUsed: 1}, {ModelID: "gpu", GPU: 20 * gb, LastUsed: 2}}
	if got := gpuIDs(gpuUnloads(cpu, 10*gb, 24*gb)); got != "gpu" {
		t.Fatalf("unloads %q, want gpu", got)
	}
}
