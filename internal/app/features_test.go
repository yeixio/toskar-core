package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/yeixio/toskar-core/internal/features"
)

// One-step setup (#1355): on an empty computer every feature says what it
// still needs, with one model to install where one fits, and nothing to
// install where nothing here can (a camera, a computer that trains).
func TestFeaturesOnAnEmptyComputer(t *testing.T) {
	ctx := context.Background()
	t.Setenv("TOSKAR_STUB_INFERENCE", "")
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	a, err := New(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })

	all := a.Features(ctx)
	byID := map[string]features.Feature{}
	for _, f := range all {
		byID[f.ID] = f
	}
	if len(all) != len(featureNeeds) || len(byID) != len(all) {
		t.Fatalf("features = %+v", all)
	}
	need := func(feature, id string) features.Need {
		t.Helper()
		for _, n := range byID[feature].Needs {
			if n.Need == id {
				return n
			}
		}
		t.Fatalf("%s has no %s need: %+v", feature, id, byID[feature])
		return features.Need{}
	}
	if f := byID["voice"]; f.Ready {
		t.Fatalf("voice is ready with no chat model: %+v", f)
	}
	if n := need("voice", "chat"); n.Ready || n.Install != "model" || n.Option == "" || n.SizeBytes == 0 {
		t.Fatalf("chat need = %+v", n)
	}
	if n := need("automationTools", "tools"); n.Install != "model" || n.Option == "" {
		t.Fatalf("tools need = %+v", n)
	}
	if n := need("meaning", "embedding"); n.Ready || n.Install != "model" || n.Option == "" {
		t.Fatalf("embedding need = %+v", n)
	}
	if n := need("door", "camera"); n.Ready || n.Install != "" {
		t.Fatalf("camera need = %+v", n)
	}
	// The door's setup downloads what it can, the camera aside.
	if f := byID["door"]; f.SetupBytes < need("door", "chat").SizeBytes {
		t.Fatalf("door downloads %d", f.SetupBytes)
	}

	connectCamera(t, a)
	if n := a.needStates(ctx)["camera"]; !n.Ready {
		t.Fatalf("camera connected: %+v", n)
	}

	if _, err := a.SetUpFeature(ctx, "nothing"); !errors.Is(err, features.ErrUnknown) {
		t.Fatalf("unknown feature: %v", err)
	}
}
