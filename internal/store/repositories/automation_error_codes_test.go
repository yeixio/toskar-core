package repositories_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/store/repositories"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A failed run keeps its error's code and values beside its text, on the
// run and on the automation, so the apps explain it and Report this sends
// the code (#588); a success clears them, and a run the daemon lost gets
// AUTOMATION_INTERRUPTED.
func TestAutomationFailuresKeepTheirCode(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	start := time.Date(2026, 10, 1, 7, 0, 0, 0, time.UTC)
	a, err := repo.Create(ctx, automations.CreateInput{
		ModelID: "model-a", Name: "Morning price", Prompt: "Check the price", ProfileID: "general-assistant",
		Schedule: automations.Schedule{Kind: automations.KindDaily, TimeZone: "UTC", Hour: 8},
	}, start)
	if err != nil {
		t.Fatal(err)
	}
	begin := func(at time.Time, lease time.Duration) automations.Run {
		t.Helper()
		run, ok, err := repo.Claim(ctx, a.ID, at, at, lease)
		if err != nil || !ok {
			t.Fatalf("claim ok=%v err=%v", ok, err)
		}
		if err := repo.MarkRunning(ctx, run.ID, at, lease); err != nil {
			t.Fatal(err)
		}
		return run
	}

	day := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	run := begin(day, time.Hour)
	failure := contracts.Failure{Text: `model "qwen3" not installed`, Code: "MODEL_NOT_INSTALLED", Details: map[string]any{"model_id": "qwen3"}}
	if err := repo.FailRun(ctx, run.ID, failure, automations.Execution{}, day.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetRun(ctx, a.ID, run.ID)
	if err != nil || got.ErrorCode != "MODEL_NOT_INSTALLED" || got.ErrorDetails["model_id"] != "qwen3" || got.Error != failure.Text {
		t.Fatalf("run = %+v, %v", got, err)
	}
	after, err := repo.Get(ctx, a.ID)
	if err != nil || after.LastErrorCode != "MODEL_NOT_INSTALLED" || after.LastErrorDetails["model_id"] != "qwen3" {
		t.Fatalf("automation = %+v, %v", after, err)
	}

	day = day.Add(24 * time.Hour)
	run = begin(day, time.Hour)
	if err := repo.CompleteRun(ctx, run.ID, automations.Execution{Text: "$12"}, day.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if after, _ = repo.Get(ctx, a.ID); after.LastError != "" || after.LastErrorCode != "" || after.LastErrorDetails != nil {
		t.Fatalf("a success kept the failure: %+v", after)
	}

	day = day.Add(24 * time.Hour)
	run = begin(day, time.Minute)
	if _, err := repo.AbandonExpired(ctx, day.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got, _ = repo.GetRun(ctx, a.ID, run.ID); got.ErrorCode != "AUTOMATION_INTERRUPTED" {
		t.Fatalf("lost run = %+v", got)
	}
}
