// Package projects keeps related chats together with what they share (#1136):
// instructions, files, a folder on this computer, a default profile and
// model, and a memory of their own. A project belongs to the person who made
// it, like their chats; another person's project reads as not found.
//
// A chat's folder is still called project in conversations.project and the
// X-Toskar-Project header. A project here always has an id.
package projects

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// ErrNotFound is a project that isn't the person's, or doesn't exist.
var ErrNotFound = contracts.NewError("PROJECT_NOT_FOUND", nil, errors.New("project not found"))

// Limits on what a person writes.
const (
	MaxNameRunes         = 200
	MaxInstructionsRunes = 20000
	MaxKnowledge         = 50
)

// Store keeps projects in the daemon database.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// NewStore returns a project store.
func NewStore(db *sql.DB) *Store { return &Store{db: db, now: time.Now} }

// Fields are a project's settings, for making one or changing it: nil leaves
// a field as it is.
type Fields struct {
	Name         *string
	Instructions *string
	Folder       *string
	ProfileID    *string
	ModelID      *string
	MemoryOff    *bool
	Knowledge    []string
	// SetKnowledge says Knowledge is given, so an empty list clears it.
	SetKnowledge bool
}

func (f Fields) apply(p *contracts.Project) error {
	if f.Name != nil {
		p.Name = strings.TrimSpace(*f.Name)
	}
	if p.Name == "" {
		return contracts.NewError("PROJECT_NAME_REQUIRED", nil, errors.New("name the project"))
	}
	if utf8.RuneCountInString(p.Name) > MaxNameRunes {
		return fmt.Errorf("keep a project's name under %d characters", MaxNameRunes)
	}
	if f.Instructions != nil {
		p.Instructions = strings.TrimSpace(*f.Instructions)
		if utf8.RuneCountInString(p.Instructions) > MaxInstructionsRunes {
			return fmt.Errorf("keep a project's instructions under %d characters", MaxInstructionsRunes)
		}
	}
	if f.Folder != nil {
		p.Folder = *f.Folder
	}
	if f.ProfileID != nil {
		p.ProfileID = strings.TrimSpace(*f.ProfileID)
	}
	if f.ModelID != nil {
		p.ModelID = strings.TrimSpace(*f.ModelID)
	}
	if f.MemoryOff != nil {
		p.MemoryOff = *f.MemoryOff
	}
	if f.SetKnowledge {
		seen := map[string]bool{}
		p.Knowledge = []string{}
		for _, id := range f.Knowledge {
			if id = strings.TrimSpace(id); id != "" && !seen[id] {
				seen[id] = true
				p.Knowledge = append(p.Knowledge, id)
			}
		}
		if len(p.Knowledge) > MaxKnowledge {
			return fmt.Errorf("link at most %d knowledge sources to a project", MaxKnowledge)
		}
	}
	return nil
}

// columns are what scan reads. A project's activity is its own last change
// or its newest chat's, whichever is later.
const columns = `p.id, p.name, p.instructions, p.folder, p.profile_id, p.model_id, p.memory_off, p.knowledge_json, p.created_at,
	p.updated_at, COALESCE((SELECT MAX(c.updated_at) FROM conversations c WHERE c.project_id = p.id AND c.person_id = p.person_id), ''),
	(SELECT COUNT(*) FROM conversations c WHERE c.project_id = p.id AND c.person_id = p.person_id)`

func scan(row interface{ Scan(...any) error }) (contracts.Project, error) {
	var p contracts.Project
	var memoryOff int
	var knowledge, created, updated, chatted string
	if err := row.Scan(&p.ID, &p.Name, &p.Instructions, &p.Folder, &p.ProfileID, &p.ModelID, &memoryOff, &knowledge, &created, &updated, &chatted, &p.ChatCount); err != nil {
		return contracts.Project{}, err
	}
	p.MemoryOff = memoryOff != 0
	if json.Unmarshal([]byte(knowledge), &p.Knowledge) != nil || p.Knowledge == nil {
		p.Knowledge = []string{}
	}
	p.Files = []contracts.ProjectFile{}
	p.CreatedAt, p.UpdatedAt = parseTS(created), parseTS(updated)
	if last := parseTS(chatted); last.After(p.UpdatedAt) {
		p.UpdatedAt = last
	}
	return p, nil
}

// List returns the person's projects, newest activity first.
func (s *Store) List(ctx context.Context) ([]contracts.Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+columns+` FROM projects p WHERE p.person_id = ?`, auth.PersonID(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []contracts.Project{}
	for rows.Next() {
		p, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

// Get returns one of the person's projects.
func (s *Store) Get(ctx context.Context, id string) (contracts.Project, error) {
	p, err := scan(s.db.QueryRowContext(ctx, `SELECT `+columns+` FROM projects p WHERE p.id = ? AND p.person_id = ?`, id, auth.PersonID(ctx)))
	if errors.Is(err, sql.ErrNoRows) {
		return contracts.Project{}, ErrNotFound
	}
	return p, err
}

// Create makes a project for the person.
func (s *Store) Create(ctx context.Context, f Fields) (contracts.Project, error) {
	now := s.now().UTC()
	p := contracts.Project{ID: uuid.NewString(), Knowledge: []string{}, Files: []contracts.ProjectFile{}, CreatedAt: now, UpdatedAt: now}
	if err := f.apply(&p); err != nil {
		return contracts.Project{}, err
	}
	knowledge, _ := json.Marshal(p.Knowledge)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO projects (id, person_id, name, instructions, folder, profile_id, model_id, memory_off, knowledge_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, auth.PersonID(ctx), p.Name, p.Instructions, p.Folder, p.ProfileID, p.ModelID, boolInt(p.MemoryOff), string(knowledge), ts(now), ts(now))
	if err != nil {
		return contracts.Project{}, err
	}
	return p, nil
}

// Update changes one of the person's projects.
func (s *Store) Update(ctx context.Context, id string, f Fields) (contracts.Project, error) {
	p, err := s.Get(ctx, id)
	if err != nil {
		return contracts.Project{}, err
	}
	if err := f.apply(&p); err != nil {
		return contracts.Project{}, err
	}
	knowledge, _ := json.Marshal(p.Knowledge)
	if _, err := s.db.ExecContext(ctx, `
		UPDATE projects SET name = ?, instructions = ?, folder = ?, profile_id = ?, model_id = ?, memory_off = ?, knowledge_json = ?, updated_at = ?
		WHERE id = ? AND person_id = ?`,
		p.Name, p.Instructions, p.Folder, p.ProfileID, p.ModelID, boolInt(p.MemoryOff), string(knowledge), ts(s.now()), id, auth.PersonID(ctx)); err != nil {
		return contracts.Project{}, err
	}
	return s.Get(ctx, id)
}

// Touch marks a project changed, such as when a file is added.
func (s *Store) Touch(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE projects SET updated_at = ? WHERE id = ? AND person_id = ?`, ts(s.now()), id, auth.PersonID(ctx))
	return err
}

// ChatIDs lists the person's chats in a project.
func (s *Store) ChatIDs(ctx context.Context, id string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM conversations WHERE project_id = ? AND person_id = ?`, id, auth.PersonID(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Delete removes one of the person's projects. Its chats left in it move
// out, keeping the folder they named themselves.
func (s *Store) Delete(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	person := auth.PersonID(ctx)
	res, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE id = ? AND person_id = ?`, id, person)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `UPDATE conversations SET project_id = '' WHERE project_id = ? AND person_id = ?`, id, person); err != nil {
		return err
	}
	return tx.Commit()
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTS(s string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
