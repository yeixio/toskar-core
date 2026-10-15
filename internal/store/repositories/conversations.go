package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// ConversationRepo persists chat conversations.
type ConversationRepo struct {
	db *sql.DB
}

func NewConversationRepo(db *sql.DB) *ConversationRepo {
	return &ConversationRepo{db: db}
}

// ConversationPatch is a partial update for a conversation.
type ConversationPatch struct {
	Title     *string
	ProfileID *string
	ModelID   *string
	MemoryOff *bool
}

func (r *ConversationRepo) List(ctx context.Context) ([]contracts.Conversation, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, COALESCE(title,''), COALESCE(profile_id,''), COALESCE(model_id,''), memory_off, compact_off, created_at, updated_at
		FROM conversations WHERE person_id = ? ORDER BY updated_at DESC`, auth.PersonID(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []contracts.Conversation
	for rows.Next() {
		c, err := scanConversation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if out == nil {
		out = []contracts.Conversation{}
	}
	return out, rows.Err()
}

func (r *ConversationRepo) Get(ctx context.Context, id string) (contracts.Conversation, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, COALESCE(title,''), COALESCE(profile_id,''), COALESCE(model_id,''), memory_off, compact_off, created_at, updated_at
		FROM conversations WHERE id = ? AND person_id = ?`, id, auth.PersonID(ctx))
	c, err := scanConversation(row)
	if err == sql.ErrNoRows {
		return contracts.Conversation{}, fmt.Errorf("conversation %q not found", id)
	}
	return c, err
}

func (r *ConversationRepo) Create(ctx context.Context, title, profileID, modelID string) (contracts.Conversation, error) {
	if title == "" {
		title = "New chat"
	}
	now := time.Now().UTC()
	c := contracts.Conversation{
		ID:        uuid.NewString(),
		Title:     title,
		ProfileID: profileID,
		ModelID:   modelID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO conversations (id, title, profile_id, model_id, created_at, updated_at, person_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Title, nullIfEmpty(c.ProfileID), nullIfEmpty(c.ModelID),
		c.CreatedAt.Format(time.RFC3339Nano), c.UpdatedAt.Format(time.RFC3339Nano), auth.PersonID(ctx))
	return c, err
}

func (r *ConversationRepo) Update(ctx context.Context, id string, patch ConversationPatch) (contracts.Conversation, error) {
	existing, err := r.Get(ctx, id)
	if err != nil {
		return contracts.Conversation{}, err
	}
	if patch.Title != nil {
		existing.Title = *patch.Title
		if existing.Title == "" {
			existing.Title = "New chat"
		}
	}
	if patch.ProfileID != nil {
		existing.ProfileID = *patch.ProfileID
	}
	if patch.ModelID != nil {
		existing.ModelID = *patch.ModelID
	}
	if patch.MemoryOff != nil {
		existing.MemoryOff = *patch.MemoryOff
	}
	memoryOff := 0
	if existing.MemoryOff {
		memoryOff = 1
	}
	existing.UpdatedAt = time.Now().UTC()
	_, err = r.db.ExecContext(ctx, `
		UPDATE conversations
		SET title = ?, profile_id = ?, model_id = ?, memory_off = ?, updated_at = ?
		WHERE id = ? AND person_id = ?`,
		existing.Title, nullIfEmpty(existing.ProfileID), nullIfEmpty(existing.ModelID), memoryOff,
		existing.UpdatedAt.Format(time.RFC3339Nano), id, auth.PersonID(ctx))
	if err != nil {
		return contracts.Conversation{}, err
	}
	return existing, nil
}

// SetCompactOff keeps a conversation verbatim, or lets auto-compact
// summarize it again (#342).
func (r *ConversationRepo) SetCompactOff(ctx context.Context, id string, off bool) error {
	res, err := r.db.ExecContext(ctx, `UPDATE conversations SET compact_off = ? WHERE id = ? AND person_id = ?`, off, id, auth.PersonID(ctx))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("conversation %q not found", id)
	}
	return nil
}

func (r *ConversationRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM conversations WHERE id = ? AND person_id = ?`, id, auth.PersonID(ctx))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("conversation %q not found", id)
	}
	return nil
}

// DeleteMany removes the request's person's conversations among ids, with
// their messages, in one transaction (#452), and returns those it removed.
// An id that isn't one of theirs is left alone.
func (r *ConversationRepo) DeleteMany(ctx context.Context, ids []string) ([]string, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	person := auth.PersonID(ctx)
	deleted := []string{}
	for _, id := range ids {
		res, err := tx.ExecContext(ctx, `DELETE FROM conversations WHERE id = ? AND person_id = ?`, id, person)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			deleted = append(deleted, id)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return deleted, nil
}

// DeleteAll removes every conversation and message of the request's person.
func (r *ConversationRepo) DeleteAll(ctx context.Context) error {
	person := auth.PersonID(ctx)
	if _, err := r.db.ExecContext(ctx, `DELETE FROM messages WHERE conversation_id IN (SELECT id FROM conversations WHERE person_id = ?)`, person); err != nil {
		return err
	}
	if _, err := r.db.ExecContext(ctx, `DELETE FROM message_shown WHERE conversation_id IN (SELECT id FROM conversations WHERE person_id = ?)`, person); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `DELETE FROM conversations WHERE person_id = ?`, person)
	return err
}

type conversationScanner interface {
	Scan(dest ...any) error
}

func scanConversation(s conversationScanner) (contracts.Conversation, error) {
	var c contracts.Conversation
	var created, updated string
	var memoryOff, compactOff int
	if err := s.Scan(&c.ID, &c.Title, &c.ProfileID, &c.ModelID, &memoryOff, &compactOff, &created, &updated); err != nil {
		return contracts.Conversation{}, err
	}
	c.MemoryOff, c.CompactOff = memoryOff != 0, compactOff != 0
	c.CreatedAt = parseTime(created)
	c.UpdatedAt = parseTime(updated)
	return c, nil
}

func parseTime(s string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
