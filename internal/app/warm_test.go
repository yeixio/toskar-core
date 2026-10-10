package app

import (
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestWarmStatus(t *testing.T) {
	const gb = 1 << 30
	installed := []contracts.Model{
		{ID: "llama-3b", Installed: true, MemoryNeeded: 3 * gb},
		{ID: "qwen-7b", Installed: true, MemoryNeeded: 6 * gb},
		{ID: "catalog-only", Installed: false, MemoryNeeded: 2 * gb},
	}
	loaded := []contracts.RunningModelView{{ModelID: "qwen-7b", MemoryBytes: 6 * gb}}
	cases := []struct {
		name       string
		model      string
		running    []contracts.RunningModelView
		inflight   bool
		generating bool
		memTotal   uint64
		want       string
	}{
		{"cold, plenty of room", "llama-3b", nil, false, false, 16 * gb, "load"},
		{"already loaded", "qwen-7b", loaded, false, false, 16 * gb, "loaded"},
		{"loaded even while answering", "qwen-7b", loaded, false, true, 16 * gb, "loaded"},
		{"another warm-up is loading it", "llama-3b", nil, true, false, 16 * gb, "loading"},
		{"a model is answering", "llama-3b", loaded, false, true, 16 * gb, "busy"},
		{"fits beside the loaded one", "llama-3b", loaded, false, false, 16 * gb, "load"},
		{"wouldn't fit beside the loaded one", "llama-3b", loaded, false, false, 10 * gb, "no_room"},
		{"memory unknown lets it load", "llama-3b", loaded, false, false, 0, "load"},
		{"not installed here", "catalog-only", nil, false, false, 16 * gb, "not_local"},
		{"unknown model", "nope", nil, false, false, 16 * gb, "not_local"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := warmStatus(c.model, installed, c.running, c.inflight, c.generating, c.memTotal); got != c.want {
				t.Fatalf("warmStatus = %q, want %q", got, c.want)
			}
		})
	}
}
