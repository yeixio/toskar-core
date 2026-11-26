package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/speech"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/store/repositories"
	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

func voiceLanguageApp(t *testing.T) *App {
	t.Helper()
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
	return &App{DB: db, Config: cfg, Settings: repositories.NewSettingsRepo(db.SQL),
		voiceWorker: &speech.Worker{Engine: &speech.Engine{Dir: t.TempDir()}, Plain: true}}
}

// An answer read aloud in a language this computer has no voice for, such
// as Japanese, is written in one it has, and starts by saying why; a
// written answer stays in the person's language (#1405).
func TestSpokenAnswerNeedsAVoice(t *testing.T) {
	a := voiceLanguageApp(t)
	ctx := context.Background()
	asked := "日本で一番高い山は何ですか？"
	if d := a.replyLanguage(ctx, "", asked, ""); d.Tag != "ja" || d.Unspoken != "" {
		t.Fatalf("written: %+v", d)
	}
	d := a.replyLanguage(turnopts.WithSpoken(ctx), "", asked, "")
	// English, or this computer's own English, such as en-US.
	if !strings.HasPrefix(d.Tag, "en") || d.Unspoken != "ja" || !strings.Contains(d.Instruction(), "can't speak Japanese aloud") {
		t.Fatalf("spoken: %+v, %q", d, d.Instruction())
	}
	// A language it has a voice for is answered in, as before.
	if d := a.replyLanguage(turnopts.WithSpoken(ctx), "", "¿Cuál es la montaña más alta de España?", ""); d.Tag != "es" || d.Unspoken != "" {
		t.Fatalf("Spanish: %+v", d)
	}
}

// An Admin can have voice always hear one language, for a household that
// speaks only it; voice then hears and answers in it (#1405).
func TestVoiceLanguageSetting(t *testing.T) {
	a := voiceLanguageApp(t)
	ctx := context.Background()
	if got := a.voiceLanguage(ctx); got != "" {
		t.Fatalf("default %q", got)
	}
	if err := a.applySettingsPatch(ctx, map[string]any{"voice_language": "de"}); err != nil {
		t.Fatal(err)
	}
	if got := a.voiceLanguage(ctx); got != "de" {
		t.Fatalf("chosen %q", got)
	}
	err := a.applySettingsPatch(ctx, map[string]any{"voice_language": "klingon"})
	if code, _ := contracts.ErrorCode(err); code != "INVALID_SETTING" {
		t.Fatalf("an unknown language: %v", err)
	}
	if err := a.applySettingsPatch(ctx, map[string]any{"voice_language": ""}); err != nil {
		t.Fatal(err)
	}
	if got := a.voiceLanguage(ctx); got != "" {
		t.Fatalf("back to detecting %q", got)
	}
}
