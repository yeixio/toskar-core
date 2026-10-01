// Package retention removes old run records (spec §63). Run records hold
// prompts and tool results: tasks and their steps, automation run results,
// run traces, and the record of what left this computer. Chats are kept or not by the
// chat history setting and are not touched here.
package retention

import (
	"context"
	"database/sql"
	"time"
)

// DefaultDays is how long run records are kept unless changed.
const DefaultDays = 30

// Counts are how many records were removed, by kind.
type Counts struct {
	Tasks          int64 `json:"tasks"`
	Runs           int64 `json:"runs"`
	AutomationRuns int64 `json:"automation_runs"`
	Egress         int64 `json:"egress"`
}

// Before removes run records older than cutoff. Each automation keeps its
// latest successful run, which "notify when the result changes" compares
// against, and work that may still be running is never removed.
func Before(ctx context.Context, db *sql.DB, cutoff time.Time) (Counts, error) {
	return remove(ctx, db, cutoff.UTC().Format(time.RFC3339Nano))
}

// All removes every run record, with the same exceptions as Before.
func All(ctx context.Context, db *sql.DB) (Counts, error) {
	// Later than any stored timestamp.
	return remove(ctx, db, "9999")
}

func remove(ctx context.Context, db *sql.DB, cutoff string) (Counts, error) {
	var c Counts
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return c, err
	}
	defer func() { _ = tx.Rollback() }()
	exec := func(dst *int64, q string, args ...any) error {
		res, err := tx.ExecContext(ctx, q, args...)
		if err != nil {
			return err
		}
		*dst, _ = res.RowsAffected()
		return nil
	}
	// task_steps go with their tasks. Chat turns leave their task pending,
	// so only a task from the last hour that is not finished is kept, in
	// case it is still running.
	recent := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	if err := exec(&c.Tasks, `DELETE FROM tasks WHERE created_at < ? AND NOT (status IN ('pending', 'running') AND created_at > ?)`, cutoff, recent); err != nil {
		return c, err
	}
	if err := exec(&c.AutomationRuns, `
		DELETE FROM automation_runs
		WHERE created_at < ? AND status IN ('succeeded', 'failed')
		AND id NOT IN (
			SELECT id FROM automation_runs r
			WHERE r.status = 'succeeded' AND r.occurrence_at = (
				SELECT MAX(occurrence_at) FROM automation_runs x
				WHERE x.automation_id = r.automation_id AND x.status = 'succeeded'))`, cutoff); err != nil {
		return c, err
	}
	if err := exec(&c.Egress, `DELETE FROM egress WHERE at < ?`, cutoff); err != nil {
		return c, err
	}
	// Run traces (§35); one still being written has no completion time.
	if err := exec(&c.Runs, `DELETE FROM runs WHERE started_at < ? AND completed_at IS NOT NULL`, cutoff); err != nil {
		return c, err
	}
	return c, tx.Commit()
}
