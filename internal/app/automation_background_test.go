package app

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/store/repositories"
)

func TestSavedScheduleKeepsTheDaemonRunning(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	settings := repositories.NewSettingsRepo(db.SQL)
	repo := repositories.NewAutomationRepo(db.SQL)
	application := &App{Settings: settings, Automations: repo}

	if err := application.enableBackgroundWhenScheduled(ctx); err != nil {
		t.Fatal(err)
	}
	on, err := settings.GetBool(ctx, "keep_running_in_background", false)
	if err != nil {
		t.Fatal(err)
	}
	if on {
		t.Fatal("background mode turned on with no schedules")
	}

	if _, err := repo.Create(ctx, automations.CreateInput{
		ModelID:  "model-a",
		Name:     "Morning price",
		Prompt:   "Check the price",
		Schedule: automations.Schedule{Kind: automations.KindDaily, TimeZone: "UTC", Hour: 8},
	}, time.Date(2026, 9, 28, 7, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := application.enableBackgroundWhenScheduled(ctx); err != nil {
		t.Fatal(err)
	}
	on, err = settings.GetBool(ctx, "keep_running_in_background", false)
	if err != nil {
		t.Fatal(err)
	}
	if !on {
		t.Fatal("background mode stayed off after a schedule was saved")
	}
}

// Turning it off sticks, even with schedules saved (#1209).
func TestBackgroundChoiceSticks(t *testing.T) {
	ctx := context.Background()
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	application, err := New(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.DB.Close() })

	if _, err := application.Automations.Create(ctx, automations.CreateInput{
		ModelID:  "model-a",
		Name:     "Morning price",
		Prompt:   "Check the price",
		Schedule: automations.Schedule{Kind: automations.KindDaily, TimeZone: "UTC", Hour: 8},
	}, time.Date(2026, 9, 28, 7, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := application.applySettingsPatch(ctx, map[string]any{"keep_running_in_background": false}); err != nil {
		t.Fatal(err)
	}
	// As at every start.
	if err := application.enableBackgroundWhenScheduled(ctx); err != nil {
		t.Fatal(err)
	}
	if on, _ := application.Settings.GetBool(ctx, "keep_running_in_background", false); on {
		t.Fatal("background mode was turned back on after the person turned it off")
	}
}

// An automation's files are kept as long as its runs, not a week (#1209).
func TestAutomationFilesKeptWithTheirRuns(t *testing.T) {
	ctx := context.Background()
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	application, err := New(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.DB.Close() })

	save := func(source string) time.Duration {
		t.Helper()
		f, err := application.Artifacts.Save(ctx, artifacts.Input{Name: "door.jpg", Producer: "automation", SourceTask: source, Data: []byte("x")})
		if err != nil {
			t.Fatal(err)
		}
		if f.ExpiresAt == nil {
			return 0
		}
		return f.ExpiresAt.Sub(f.CreatedAt).Round(time.Hour)
	}
	if got := save("automation:a1"); got != 30*24*time.Hour {
		t.Fatalf("an automation's file is kept %v, want the 30 days its runs are", got)
	}
	if got := save(""); got != artifacts.UnfiledTTL {
		t.Fatalf("another unfiled file is kept %v", got)
	}
	if err := application.Settings.SetInt(ctx, retentionKey, 0); err != nil {
		t.Fatal(err)
	}
	if got := save("automation:a1"); got != 0 {
		t.Fatalf("with run records kept, an automation's file expires after %v", got)
	}
}
