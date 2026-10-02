package tools

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// Audit statuses: what became of a call (Gungnir §13).
const (
	RunCompleted = "completed"
	RunCached    = "cached"
	RunFailed    = "failed"
	RunDenied    = "denied"
	RunRefused   = "refused"
	RunDisabled  = "disabled"
)

// How a call was allowed.
const (
	ApprovedByProfile = "profile"
	ApprovedByYou     = "you"
	ApprovedSession   = "session"
)

// ToolRun is one audited call.
type ToolRun struct {
	ID         string    `json:"id"`
	At         time.Time `json:"at"`
	ToolID     string    `json:"tool_id"`
	Status     string    `json:"status"`
	Approval   string    `json:"approval,omitempty"`
	DurationMS int64     `json:"duration_ms"`
	// Summary is what the call was about (a query, an address, a path),
	// never a file's contents.
	Summary        string `json:"summary,omitempty"`
	Error          string `json:"error,omitempty"`
	Source         string `json:"source,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	TaskID         string `json:"task_id,omitempty"`
}

// AuditLog stores tool runs.
type AuditLog struct {
	db  *sql.DB
	now func() time.Time
}

// NewAuditLog returns an audit log in db.
func NewAuditLog(db *sql.DB) *AuditLog { return &AuditLog{db: db, now: time.Now} }

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Record stores a run. It never fails the call it describes.
func (l *AuditLog) Record(ctx context.Context, r ToolRun) {
	if l == nil || l.db == nil {
		return
	}
	if r.At.IsZero() {
		r.At = l.now()
	}
	_, _ = l.db.ExecContext(context.WithoutCancel(ctx), `
		INSERT INTO tool_runs (id, at, tool_id, status, approval, duration_ms, summary, error, source, conversation_id, task_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), r.At.UTC().Format(time.RFC3339Nano), r.ToolID, r.Status, nullable(r.Approval), r.DurationMS,
		nullable(r.Summary), nullable(r.Error), nullable(r.Source), nullable(r.ConversationID), nullable(r.TaskID))
}

// AuditFilter narrows a listing.
type AuditFilter struct {
	ToolID         string
	ConversationID string
	Limit          int
}

// List returns runs, newest first.
func (l *AuditLog) List(ctx context.Context, f AuditFilter) ([]ToolRun, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	q := `SELECT id, at, tool_id, status, COALESCE(approval, ''), duration_ms, COALESCE(summary, ''), COALESCE(error, ''),
		COALESCE(source, ''), COALESCE(conversation_id, ''), COALESCE(task_id, '') FROM tool_runs WHERE 1 = 1`
	var args []any
	if f.ToolID != "" {
		q += ` AND tool_id = ?`
		args = append(args, Canonical(f.ToolID))
	}
	if f.ConversationID != "" {
		q += ` AND conversation_id = ?`
		args = append(args, f.ConversationID)
	}
	q += ` ORDER BY at DESC LIMIT ?`
	rows, err := l.db.QueryContext(ctx, q, append(args, f.Limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ToolRun{}
	for rows.Next() {
		var r ToolRun
		var at string
		if err := rows.Scan(&r.ID, &at, &r.ToolID, &r.Status, &r.Approval, &r.DurationMS, &r.Summary, &r.Error, &r.Source, &r.ConversationID, &r.TaskID); err != nil {
			return nil, err
		}
		r.At, _ = time.Parse(time.RFC3339Nano, at)
		out = append(out, r)
	}
	return out, rows.Err()
}
