package app

import (
	"context"
	"testing"
)

// Smart Turn is off until an Admin turns it on (#1460): voice mode and the
// door then end a turn with the pause alone.
func TestSmartTurnIsOffUntilTurnedOn(t *testing.T) {
	t.Setenv("YGGDRASIL_DISCOVERY_ENABLED", "false")
	a, err := New(Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Shutdown(context.Background()) })
	ctx := context.Background()

	view, err := a.settingsView(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if view.VoiceSmartTurn || a.smartTurnOn(ctx) {
		t.Fatal("Smart Turn is on by default")
	}
	if err := a.applySettingsPatch(ctx, map[string]any{"voice_smart_turn": true}); err != nil {
		t.Fatal(err)
	}
	if view, _ = a.settingsView(ctx); !view.VoiceSmartTurn || !a.smartTurnOn(ctx) {
		t.Fatal("turning Smart Turn on didn't keep")
	}
	if err := a.applySettingsPatch(ctx, map[string]any{"voice_smart_turn": false}); err != nil {
		t.Fatal(err)
	}
	if a.smartTurnOn(ctx) {
		t.Fatal("turning Smart Turn off didn't keep")
	}
}
