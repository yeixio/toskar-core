package app

import (
	"context"
	"slices"
	"testing"
)

// A model kept running is listed as one, the idle sweep leaves it loaded,
// and turning it off lets the sweep unload it again (#966).
func TestAlwaysRunningChoice(t *testing.T) {
	a, _, _ := pictureApp(t, false)
	ctx := context.Background()
	listed := func(id string) bool {
		list, err := a.listModelsCluster(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range list {
			if m.ID == id {
				return m.AlwaysRunning
			}
		}
		t.Fatalf("%s isn't listed", id)
		return false
	}
	if listed("text-model") || a.keepLoaded("text-model") {
		t.Fatal("kept running before it was asked")
	}
	if _, err := a.setAlwaysRunning(ctx, "text-model", true); err != nil {
		t.Fatal(err)
	}
	if !listed("text-model") || !a.keepLoaded("text-model") {
		t.Fatal("not kept running")
	}
	if listed("see-model") || a.keepLoaded("see-model") {
		t.Fatal("another model kept running too")
	}
	if _, err := a.setAlwaysRunning(ctx, "text-model", false); err != nil {
		t.Fatal(err)
	}
	if listed("text-model") || a.keepLoaded("text-model") {
		t.Fatal("still kept running after turning it off")
	}
	if _, err := a.setAlwaysRunning(ctx, "no-such-model", true); err == nil {
		t.Fatal("a model that isn't installed was kept running")
	}
}

// The keeper loads each model kept running that isn't loaded, other than
// one the person stopped.
func TestPinnedToLoad(t *testing.T) {
	stopped := func(id string) bool { return id == "stopped" }
	got := pinnedToLoad([]string{"loaded", "crashed", "stopped", "new"}, []string{"loaded", "other"}, stopped)
	if !slices.Equal(got, []string{"crashed", "new"}) {
		t.Fatalf("load %v", got)
	}
	if got := pinnedToLoad(nil, []string{"loaded"}, stopped); len(got) != 0 {
		t.Fatalf("nothing kept running, yet load %v", got)
	}
}

// Only loaded models kept running count against a model that needs
// loading; a model loaded or kept running itself has its room (#966).
func TestPinnedBeside(t *testing.T) {
	if got := pinnedBeside("big", []string{"voice", "stopped"}, []string{"voice", "other"}); !slices.Equal(got, []string{"voice"}) {
		t.Fatalf("beside big: %v", got)
	}
	if got := pinnedBeside("voice", []string{"voice", "doorbell"}, []string{"voice", "doorbell"}); got != nil {
		t.Fatalf("a model kept running itself: %v", got)
	}
	if got := pinnedBeside("other", []string{"voice"}, []string{"voice", "other"}); got != nil {
		t.Fatalf("a model already loaded: %v", got)
	}
}

// Memory left beside the models kept running is this computer's memory
// less theirs, and unknown when this computer's memory is (#966).
func TestNoRoomBesidePinned(t *testing.T) {
	a, _, _ := pictureApp(t, false)
	ctx := context.Background()
	if a.noRoomBesidePinned(ctx, "see-model") {
		t.Fatal("no room with nothing kept running")
	}
	a.memTotal.Store(64 << 30)
	if left := a.memoryLeftBesidePinned(ctx, []string{"text-model"}); left != 64<<30-1 {
		t.Fatalf("memory left %d", left)
	}
	a.memTotal.Store(1)
	if left := a.memoryLeftBesidePinned(ctx, []string{"text-model"}); left != 0 {
		t.Fatalf("memory left beside a model using it all: %d", left)
	}
}
