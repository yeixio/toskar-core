package app

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A chat that names no profile, from the phone or the API, starts with the
// one chosen for new chats; deleting that profile goes back to General
// Assistant, and one that doesn't exist can't be chosen.
func TestDefaultProfileStartsNewChats(t *testing.T) {
	t.Setenv("TOSKAR_STUB_INFERENCE", "1")
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	a, err := New(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })
	ctx := context.Background()

	if got := a.defaultProfileID(ctx); got != profiles.PresetGeneral {
		t.Fatalf("out of the box: %q", got)
	}
	shop, err := a.Profiles.Create(ctx, profiles.Profile{Name: "Shop help", OrchestratorID: "simple", Roles: []contracts.ModelRole{{Role: "assistant"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.applySettingsPatch(ctx, map[string]any{"default_profile_id": shop.ID}); err != nil {
		t.Fatal(err)
	}
	if got := a.defaultProfileID(ctx); got != shop.ID {
		t.Fatalf("chosen: %q", got)
	}
	if err := a.applySettingsPatch(ctx, map[string]any{"default_profile_id": "gone"}); err == nil {
		t.Fatal("chose a profile that doesn't exist")
	} else if code, _ := contracts.ErrorCode(err); code != "PROFILE_NOT_FOUND" {
		t.Fatalf("missing profile: %v", err)
	}
	if err := a.deleteProfile(ctx, shop.ID); err != nil {
		t.Fatal(err)
	}
	if id, _ := a.Settings.GetString(ctx, "default_profile_id", ""); id != "" {
		t.Errorf("setting still %q", id)
	}
	if got := a.defaultProfileID(ctx); got != profiles.PresetGeneral {
		t.Errorf("after deleting it: %q", got)
	}
}
