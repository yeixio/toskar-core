package auth

import (
	"context"
	"database/sql"
	"time"
)

// Activity is the log of changes to people (#592): who added, changed,
// disabled, deleted, or reset whom, or revoked their keys and devices. It
// never holds a password.
type Activity struct {
	db *sql.DB
}

// NewActivity keeps the log in db.
func NewActivity(db *sql.DB) *Activity { return &Activity{db: db} }

// ActivityEntry is one change to a person.
type ActivityEntry struct {
	At         time.Time `json:"at"`
	ActorID    string    `json:"actor_id"`
	ActorName  string    `json:"actor_name"`
	TargetID   string    `json:"target_id"`
	TargetName string    `json:"target_name"`
	// Action is added, changed, disabled, enabled, deleted, password_set,
	// link_made, key_revoked, or device_revoked.
	Action string `json:"action"`
	// Detail says what changed, such as "role member → admin".
	Detail string `json:"detail,omitempty"`
}

// Record logs that the request's person did action to target.
func (a *Activity) Record(ctx context.Context, target Person, action, detail string) {
	if a == nil {
		return
	}
	actor := PrincipalFrom(ctx).Person
	_, _ = a.db.ExecContext(ctx, `INSERT INTO people_activity (at, actor_id, actor_name, target_id, target_name, action, detail) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		stamp(time.Now()), actor.ID, actor.Name, target.ID, target.Name, action, detail)
}

// List is the newest entries first, about one person when personID is set.
func (a *Activity) List(ctx context.Context, personID string, limit int) ([]ActivityEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := a.db.QueryContext(ctx, `SELECT at, actor_id, actor_name, target_id, target_name, action, detail FROM people_activity
		WHERE ? = '' OR target_id = ? OR actor_id = ? ORDER BY id DESC LIMIT ?`, personID, personID, personID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ActivityEntry{}
	for rows.Next() {
		var e ActivityEntry
		var at string
		if err := rows.Scan(&at, &e.ActorID, &e.ActorName, &e.TargetID, &e.TargetName, &e.Action, &e.Detail); err != nil {
			return nil, err
		}
		e.At = parseTime(at)
		out = append(out, e)
	}
	return out, rows.Err()
}
