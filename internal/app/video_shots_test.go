package app

import (
	"testing"

	"github.com/yeixio/toskar-core/internal/imagegen"
)

// A longer video's card says how long it is, and how long it takes here
// only once this computer has made a clip (#790).
func TestShotsEstimate(t *testing.T) {
	a := &App{videoEngine: &imagegen.Engine{WorkDir: t.TempDir()}}
	got := a.shotsEstimate(map[string]any{"shots": []any{"Sunrise", map[string]any{"prompt": "Boats", "seconds": float64(3)}}})
	if got["video_seconds"] != float64(8) {
		t.Fatalf("%v", got)
	}
	if _, ok := got["estimate_minutes"]; ok {
		t.Fatalf("an estimate before any clip: %v", got)
	}
	if got := a.shotsEstimate(map[string]any{"shots": "nope"}); got != nil {
		t.Fatalf("bad shots: %v", got)
	}
	if got := (&App{}).shotsEstimate(map[string]any{"shots": []any{"Rain"}}); got["video_seconds"] != float64(5) || got["estimate_minutes"] != nil {
		t.Fatalf("no engine: %v", got)
	}
}
