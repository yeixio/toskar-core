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
	if err := a.setAlwaysRunning(ctx, "text-model", true); err != nil {
		t.Fatal(err)
	}
	if !listed("text-model") || !a.keepLoaded("text-model") {
		t.Fatal("not kept running")
	}
	if listed("see-model") || a.keepLoaded("see-model") {
		t.Fatal("another model kept running too")
	}
	if err := a.setAlwaysRunning(ctx, "text-model", false); err != nil {
		t.Fatal(err)
	}
	if listed("text-model") || a.keepLoaded("text-model") {
		t.Fatal("still kept running after turning it off")
	}
	if err := a.setAlwaysRunning(ctx, "no-such-model", true); err == nil {
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
