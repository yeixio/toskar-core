package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/store/repositories"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Voice hears fast by default, and accurately, with Whisper small, when an
// Admin chooses it for noisier rooms and far microphones (#1330).
func TestVoiceHearingSetting(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg, err := config.NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{DB: db, Config: cfg, Settings: repositories.NewSettingsRepo(db.SQL)}
	ctx := context.Background()
	if got := a.voiceHearing(ctx); got != "fast" {
		t.Fatalf("default %q", got)
	}
	if err := a.applySettingsPatch(ctx, map[string]any{"voice_hearing": "accurate"}); err != nil {
		t.Fatal(err)
	}
	if got := a.voiceHearing(ctx); got != "accurate" {
		t.Fatalf("chosen %q", got)
	}
	err = a.applySettingsPatch(ctx, map[string]any{"voice_hearing": "large"})
	if code, _ := contracts.ErrorCode(err); code != "INVALID_SETTING" {
		t.Fatalf("an unknown quality: %v", err)
	}
	if got := a.voiceHearing(ctx); got != "accurate" {
		t.Fatalf("after a bad value %q", got)
	}
}
