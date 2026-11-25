package repositories_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/store/repositories"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Clearing one automation's history (#1335) leaves the others' runs and a
// run still working; a single run that is still working can't be deleted.
func TestClearAutomationRuns(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := repositories.NewAutomationRepo(db.SQL)
	ctx := context.Background()
	start := time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)
	newAuto := func(name string) automations.Automation {
		t.Helper()
		a, err := repo.Create(ctx, automations.CreateInput{
			ModelID: "model-a", Name: name, Prompt: "Look at the door",
			Schedule: automations.Schedule{Kind: automations.KindDaily, TimeZone: "UTC", Hour: 8},
		}, start)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	doorbell, other := newAuto("Doorbell"), newAuto("Other")
	run := func(a automations.Automation, at time.Time, finish string) automations.Run {
		t.Helper()
		r, ok, err := repo.Claim(ctx, a.ID, at, at, time.Hour)
		if err != nil || !ok {
			t.Fatalf("claim ok=%v err=%v", ok, err)
		}
		if err := repo.MarkRunning(ctx, r.ID, at, time.Hour); err != nil {
			t.Fatal(err)
		}
		switch finish {
		case "ok":
			err = repo.CompleteRun(ctx, r.ID, automations.Execution{Text: "someone's there"}, at.Add(time.Minute))
		case "fail":
			err = repo.FailRun(ctx, r.ID, contracts.Failure{Text: "no camera", Code: "AUTOMATION_FAILED"}, automations.Execution{}, at.Add(time.Minute))
		}
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	run(doorbell, start.Add(1*time.Hour), "ok")
	run(doorbell, start.Add(2*time.Hour), "fail")
	single := run(doorbell, start.Add(3*time.Hour), "ok")
	working := run(doorbell, start.Add(4*time.Hour), "")
	run(other, start.Add(1*time.Hour), "ok")

	if err := repo.DeleteRun(ctx, doorbell.ID, working.ID); !errors.Is(err, automations.ErrRunInProgress) {
		t.Fatalf("delete a running run = %v, want ErrRunInProgress", err)
	}
	if err := repo.DeleteRun(ctx, doorbell.ID, single.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteRun(ctx, other.ID, working.ID); err == nil {
		t.Fatal("deleted another automation's run")
	}
	n, err := repo.ClearRuns(ctx, doorbell.ID)
	if err != nil || n != 2 {
		t.Fatalf("cleared %d, %v; want 2", n, err)
	}
	left, err := repo.ListRuns(ctx, doorbell.ID)
	if err != nil || len(left) != 1 || left[0].ID != working.ID {
		t.Fatalf("doorbell runs left = %+v, %v; want only the working one", left, err)
	}
	others, err := repo.ListRuns(ctx, other.ID)
	if err != nil || len(others) != 1 {
		t.Fatalf("other runs = %d, %v; want 1", len(others), err)
	}
	if _, err := repo.ClearRuns(ctx, "missing"); err == nil {
		t.Fatal("cleared a missing automation")
	}
}
