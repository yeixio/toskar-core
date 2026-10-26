package app

import (
	"context"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/models/lifecycle"
)

// A model loading for a request isn't idle, though its last use, the
// previous instance's, is long past: the sweep unloaded Qwen 3.8 while it
// loaded for a triage request, and the request moved to another model.
func TestIdleSweepLeavesAModelThatIsLoading(t *testing.T) {
	a := &App{}
	now := time.Now()
	var stopped []string
	sweep := &lifecycle.Sweeper{
		List: func(context.Context) ([]lifecycle.Instance, error) {
			// The previous instance's last use, 40 minutes ago.
			return []lifecycle.Instance{{InstanceID: "new", ModelID: "qwen", LastUsed: now.Add(-40 * time.Minute)}}, nil
		},
		Stop: func(_ context.Context, id string) error {
			stopped = append(stopped, id)
			return nil
		},
		Settings: func(context.Context) (string, int, error) { return "automatic", 15, nil },
		Keep:     func(_ context.Context, inst lifecycle.Instance) bool { return a.keepLoaded(inst.ModelID) },
	}

	loaded := a.loadingModel("qwen")
	sweep.TickOnce(context.Background(), now)
	if len(stopped) != 0 {
		t.Fatalf("unloaded while loading: %v", stopped)
	}
	// Two loads of one model: it stays kept until both are done.
	second := a.loadingModel("qwen")
	loaded()
	if !a.keepLoaded("qwen") || a.keepLoaded("other") {
		t.Fatal("one load still in progress")
	}
	second()
	if a.keepLoaded("qwen") {
		t.Fatal("kept after its loads finished")
	}
	// Starting a load counts as a use, so its last use is now.
	if v, ok := runtimeLastUsed.Load("qwen"); !ok || time.Since(v.(time.Time)) > time.Minute {
		t.Fatalf("last use = %v", v)
	}
	// Idle for real, it goes.
	sweep.TickOnce(context.Background(), now)
	if len(stopped) != 1 {
		t.Fatalf("stopped = %v", stopped)
	}
}
