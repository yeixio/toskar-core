package swarm

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// ErrNotFound is a swarm that isn't the person's, or doesn't exist.
var ErrNotFound = contracts.NewError("SWARM_NOT_FOUND", nil, errors.New("swarm not found"))

// ErrRunning is a swarm deleted while its agents are still working.
var ErrRunning = contracts.NewError("SWARM_RUNNING", nil, errors.New("this swarm is still running; stop it first"))

// Store keeps swarms in the daemon database. Every query is the person's
// own: another person's swarm reads as not found.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// NewStore returns a swarm store.
func NewStore(db *sql.DB) *Store { return &Store{db: db, now: time.Now} }

// result is what result_json keeps: the agents, the merge's outcome, and the
// totals.
type result struct {
	Agents    []contracts.SwarmAgent    `json:"agents"`
	Winner    int                       `json:"winner,omitempty"`
	Note      string                    `json:"note,omitempty"`
	Tokens    int                       `json:"tokens"`
	Seconds   float64                   `json:"seconds"`
	Folder    string                    `json:"folder,omitempty"`
	Branch    string                    `json:"branch,omitempty"`
	Checks    string                    `json:"checks,omitempty"`
	Conflicts []contracts.SwarmConflict `json:"conflicts,omitempty"`
	Applied   bool                      `json:"applied,omitempty"`
}

func resultOf(s contracts.Swarm) result {
	return result{Agents: s.Agents, Winner: s.Winner, Note: s.Note, Tokens: s.Tokens, Seconds: s.Seconds,
		Folder: s.Folder, Branch: s.Branch, Checks: s.Checks, Conflicts: s.Conflicts, Applied: s.Applied}
}

const columns = `id, project_id, conversation_id, mode, status, request, budget_json, result_json, created_at, finished_at`

func scan(row interface{ Scan(...any) error }) (contracts.Swarm, error) {
	var s contracts.Swarm
	var budget, res, created, finished string
	if err := row.Scan(&s.ID, &s.ProjectID, &s.ConversationID, &s.Mode, &s.Status, &s.Request, &budget, &res, &created, &finished); err != nil {
		return contracts.Swarm{}, err
	}
	_ = json.Unmarshal([]byte(budget), &s.Budget)
	var r result
	_ = json.Unmarshal([]byte(res), &r)
	s.Agents, s.Winner, s.Note, s.Tokens, s.Seconds = r.Agents, r.Winner, r.Note, r.Tokens, r.Seconds
	s.Folder, s.Branch, s.Checks, s.Conflicts, s.Applied = r.Folder, r.Branch, r.Checks, r.Conflicts, r.Applied
	if s.Agents == nil {
		s.Agents = []contracts.SwarmAgent{}
	}
	s.CreatedAt = parseTS(created)
	if t := parseTS(finished); !t.IsZero() {
		s.FinishedAt = &t
	}
	return s, nil
}

// Create starts a record of a swarm for the person: running, with no agents
// yet. The project may be filled in later, once the chat has one.
func (st *Store) Create(ctx context.Context, projectID, conversationID, mode, request string, budget contracts.SwarmBudget) (contracts.Swarm, error) {
	s := contracts.Swarm{
		ID: uuid.NewString(), ProjectID: projectID, ConversationID: conversationID, Mode: mode,
		Status: StatusRunning, Request: request, Budget: budget, Agents: []contracts.SwarmAgent{},
		CreatedAt: st.now().UTC(),
	}
	b, _ := json.Marshal(budget)
	r, _ := json.Marshal(result{Agents: s.Agents})
	_, err := st.db.ExecContext(ctx, `
		INSERT INTO swarms (id, person_id, project_id, conversation_id, mode, status, request, budget_json, result_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, auth.PersonID(ctx), projectID, conversationID, mode, s.Status, request, string(b), string(r), ts(s.CreatedAt))
	if err != nil {
		return contracts.Swarm{}, err
	}
	return s, nil
}

// Get returns one of the person's swarms.
func (st *Store) Get(ctx context.Context, id string) (contracts.Swarm, error) {
	s, err := scan(st.db.QueryRowContext(ctx, `SELECT `+columns+` FROM swarms WHERE id = ? AND person_id = ?`, id, auth.PersonID(ctx)))
	if errors.Is(err, sql.ErrNoRows) {
		return contracts.Swarm{}, ErrNotFound
	}
	return s, err
}

// List returns the person's swarms in a project, or all of theirs for "",
// newest first.
func (st *Store) List(ctx context.Context, projectID string) ([]contracts.Swarm, error) {
	q := `SELECT ` + columns + ` FROM swarms WHERE person_id = ?`
	args := []any{auth.PersonID(ctx)}
	if projectID != "" {
		q += ` AND project_id = ?`
		args = append(args, projectID)
	}
	rows, err := st.db.QueryContext(ctx, q+` ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []contracts.Swarm{}
	for rows.Next() {
		s, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Save writes a swarm's state: its project, status, mode, agents, outcome,
// and totals. A swarm that's done, stopped, or failed is finished now.
func (st *Store) Save(ctx context.Context, s contracts.Swarm) error {
	r, _ := json.Marshal(resultOf(s))
	finished := ""
	switch s.Status {
	case StatusDone, StatusStopped, StatusFailed:
		finished = ts(st.now())
	}
	res, err := st.db.ExecContext(ctx, `
		UPDATE swarms SET project_id = ?, mode = ?, status = ?, result_json = ?, finished_at = ?
		WHERE id = ? AND person_id = ?`,
		s.ProjectID, s.Mode, s.Status, string(r), finished, s.ID, auth.PersonID(ctx))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes one of the person's swarms. Its chats stay, as ordinary
// chats in its project. A running swarm is stopped first by the caller.
func (st *Store) Delete(ctx context.Context, id string) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	person := auth.PersonID(ctx)
	var status string
	err = tx.QueryRowContext(ctx, `SELECT status FROM swarms WHERE id = ? AND person_id = ?`, id, person).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status == StatusRunning || status == StatusMerging {
		return ErrRunning
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM swarms WHERE id = ? AND person_id = ?`, id, person); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE conversations SET swarm_id = '', swarm_role = '' WHERE swarm_id = ? AND person_id = ?`, id, person); err != nil {
		return err
	}
	return tx.Commit()
}

// KeptBranches are swarms, anyone's, that finished before a time and still
// have branches in a folder, for the cleanup that deletes them.
func (st *Store) KeptBranches(ctx context.Context, before time.Time) ([]contracts.Swarm, error) {
	rows, err := st.db.QueryContext(ctx, `SELECT `+columns+` FROM swarms
		WHERE created_at < ? AND status NOT IN (?, ?) AND result_json LIKE '%"folder":%'`, ts(before), StatusRunning, StatusMerging)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []contracts.Swarm
	for rows.Next() {
		s, err := scan(rows)
		if err != nil {
			return nil, err
		}
		if s.Folder != "" && hasBranches(s) {
			out = append(out, s)
		}
	}
	return out, rows.Err()
}

func hasBranches(s contracts.Swarm) bool {
	if s.Branch != "" {
		return true
	}
	for _, a := range s.Agents {
		if a.Branch != "" {
			return true
		}
	}
	return false
}

// ForgetBranches records that a swarm's branches were deleted: the record no
// longer names them. It's the cleanup's, so it isn't any one person's.
func (st *Store) ForgetBranches(ctx context.Context, id string) error {
	var raw string
	if err := st.db.QueryRowContext(ctx, `SELECT result_json FROM swarms WHERE id = ?`, id).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	var r result
	_ = json.Unmarshal([]byte(raw), &r)
	r.Branch = ""
	for i := range r.Agents {
		r.Agents[i].Branch = ""
	}
	for i := range r.Conflicts {
		r.Conflicts[i].Branch = ""
	}
	b, _ := json.Marshal(r)
	_, err := st.db.ExecContext(ctx, `UPDATE swarms SET result_json = ? WHERE id = ?`, string(b), id)
	return err
}

// FailRunning marks swarms left running when Core stopped as stopped: their
// agents' turns ended with it.
func (st *Store) FailRunning(ctx context.Context) error {
	_, err := st.db.ExecContext(ctx, `UPDATE swarms SET status = ?, finished_at = ? WHERE status IN (?, ?)`,
		StatusStopped, ts(st.now()), StatusRunning, StatusMerging)
	return err
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTS(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
