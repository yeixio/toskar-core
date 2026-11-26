package training

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBaseWeightsCached(t *testing.T) {
	home := t.TempDir()
	if BaseWeightsCached(home, "Qwen/Qwen3-0.6B") {
		t.Fatal("an empty cache has the weights")
	}
	snap := filepath.Join(home, "hub", "models--Qwen--Qwen3-0.6B", "snapshots", "abc123")
	if err := os.MkdirAll(snap, 0o755); err != nil {
		t.Fatal(err)
	}
	// Only the config, from a run that stopped before the weights came.
	if err := os.WriteFile(filepath.Join(snap, "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if BaseWeightsCached(home, "Qwen/Qwen3-0.6B") {
		t.Fatal("a config alone counts as the weights")
	}
	if err := os.WriteFile(filepath.Join(snap, "model.safetensors"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !BaseWeightsCached(home, "Qwen/Qwen3-0.6B") {
		t.Fatal("cached weights not found")
	}
	if BaseWeightsCached(home, "Qwen/Qwen3-1.7B") || BaseWeightsCached("", "Qwen/Qwen3-0.6B") {
		t.Fatal("another repo, or no cache, counts")
	}
}
