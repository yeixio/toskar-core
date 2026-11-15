package repositories

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

// Versions of a chat (#447): retrying an answer or editing a message adds
// a version of that point instead of replacing it. A message's parent is
// the one it follows, and messages with one parent are versions of that
// point; each point shows one, its latest unless another was chosen. The
// chat people see, and the model is sent, is the path of shown versions.
// Messages from before have no parent and follow the one added before
// them, so an existing chat is one version at every point.

// ErrNoMessage is a message that isn't in the conversation, or isn't the
// request's person's.
var ErrNoMessage = errors.New("no such message in this chat")

// chatTree is a conversation's messages as versions.
type chatTree struct {
	byID     map[string]contracts.Message
	children map[string][]string // by parent; "" is the first point
	shown    map[string]string   // by parent
}

// tree reads a conversation's messages in the order they were added.
// created_at can't order them: RFC3339Nano drops trailing zeros, so
// "05.12Z" sorts after "05.123Z" as text, and back-to-back messages can share
// a timestamp. The repository is the only writer, so rowid is the order.
func (r *ConversationRepo) tree(ctx context.Context, conversationID string) (chatTree, error) {
	t := chatTree{byID: map[string]contracts.Message{}, children: map[string][]string{}, shown: map[string]string{}}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, conversation_id, role, content, COALESCE(meta_json, ''), created_at, parent_id
		FROM messages WHERE conversation_id = ?
		AND conversation_id IN (SELECT id FROM conversations WHERE person_id = ?) ORDER BY rowid ASC`, conversationID, auth.PersonID(ctx))
	if err != nil {
		return t, err
	}
	defer rows.Close()
	previous := ""
	for rows.Next() {
		var m contracts.Message
		var created, meta string
		var parent sql.NullString
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Role, &m.Content, &meta, &created, &parent); err != nil {
			return t, err
		}
		m.CreatedAt = parseTime(created)
		if meta != "" {
			m.Meta = &contracts.MessageMeta{}
			if json.Unmarshal([]byte(meta), m.Meta) != nil {
				m.Meta = nil
			}
		}
		// One from before versions follows the one added before it.
		m.ParentID = previous
		if parent.Valid {
			m.ParentID = parent.String
		}
		t.byID[m.ID] = m
		t.children[m.ParentID] = append(t.children[m.ParentID], m.ID)
		previous = m.ID
	}
	if err := rows.Err(); err != nil {
		return t, err
	}
	shown, err := r.db.QueryContext(ctx, `SELECT parent_id, message_id FROM message_shown WHERE conversation_id = ?`, conversationID)
	if err != nil {
		return t, err
	}
	defer shown.Close()
	for shown.Next() {
		var parent, id string
		if err := shown.Scan(&parent, &id); err != nil {
			return t, err
		}
		t.shown[parent] = id
	}
	return t, shown.Err()
}

// shownChild is the version shown after parent, or "" at the end.
func (t chatTree) shownChild(parent string) string {
	kids := t.children[parent]
	if len(kids) == 0 {
		return ""
	}
	if id := t.shown[parent]; id != "" {
		for _, k := range kids {
			if k == id {
				return id
			}
		}
	}
	return kids[len(kids)-1]
}

// withVersions is m with its versions, when there's more than one.
func (t chatTree) withVersions(m contracts.Message) contracts.Message {
	kids := t.children[m.ParentID]
	if len(kids) > 1 {
		v := &contracts.MessageVersions{Count: len(kids), IDs: append([]string(nil), kids...)}
		for i, k := range kids {
			if k == m.ID {
				v.Index = i + 1
			}
		}
		m.Versions = v
	}
	return m
}

// shownPath is the chat as shown, first message first.
func (t chatTree) shownPath() []contracts.Message {
	out := []contracts.Message{}
	seen := map[string]bool{}
	for id := t.shownChild(""); id != "" && !seen[id]; id = t.shownChild(id) {
		seen[id] = true
		out = append(out, t.withVersions(t.byID[id]))
	}
	return out
}

// pathTo is the chat from its first message to id.
func (t chatTree) pathTo(id string) ([]contracts.Message, bool) {
	var rev []contracts.Message
	seen := map[string]bool{}
	for cur := id; cur != "" && !seen[cur]; {
		m, ok := t.byID[cur]
		if !ok {
			return nil, false
		}
		seen[cur] = true
		rev = append(rev, t.withVersions(m))
		cur = m.ParentID
	}
	out := make([]contracts.Message, 0, len(rev))
	for i := len(rev) - 1; i >= 0; i-- {
		out = append(out, rev[i])
	}
	return out, len(out) > 0
}

// ListMessages returns the chat as shown: at each point, the version
// shown, with its versions when there's more than one (#447).
func (r *ConversationRepo) ListMessages(ctx context.Context, conversationID string) ([]contracts.Message, error) {
	t, err := r.tree(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	return t.shownPath(), nil
}

// PathTo returns the chat from its first message to messageID, whichever
// versions are shown: what a turn answering from there is sent.
func (r *ConversationRepo) PathTo(ctx context.Context, conversationID, messageID string) ([]contracts.Message, error) {
	t, err := r.tree(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	path, ok := t.pathTo(messageID)
	if !ok {
		return nil, ErrNoMessage
	}
	return path, nil
}

// Message is one message of a conversation, with its parent.
func (r *ConversationRepo) Message(ctx context.Context, conversationID, messageID string) (contracts.Message, error) {
	t, err := r.tree(ctx, conversationID)
	if err != nil {
		return contracts.Message{}, err
	}
	m, ok := t.byID[messageID]
	if !ok {
		return contracts.Message{}, ErrNoMessage
	}
	return t.withVersions(m), nil
}

// ShowVersion shows messageID at its point in the chat, and with it the
// conversation that followed it, and returns the chat as shown.
func (r *ConversationRepo) ShowVersion(ctx context.Context, conversationID, messageID string) ([]contracts.Message, error) {
	t, err := r.tree(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	m, ok := t.byID[messageID]
	if !ok {
		return nil, ErrNoMessage
	}
	if err := r.setShown(ctx, conversationID, m.ParentID, messageID); err != nil {
		return nil, err
	}
	t.shown[m.ParentID] = messageID
	return t.shownPath(), nil
}

func (r *ConversationRepo) setShown(ctx context.Context, conversationID, parentID, messageID string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO message_shown (conversation_id, parent_id, message_id) VALUES (?, ?, ?)
		ON CONFLICT(conversation_id, parent_id) DO UPDATE SET message_id = excluded.message_id`, conversationID, parentID, messageID)
	return err
}

// AddMessage adds a message at the end of the chat as shown.
func (r *ConversationRepo) AddMessage(ctx context.Context, conversationID, role, content string) (contracts.Message, error) {
	return r.AddMessageWithMeta(ctx, conversationID, role, content, nil)
}

// AddMessageWithMeta adds a message, with the sources and steps behind it,
// at the end of the chat as shown.
func (r *ConversationRepo) AddMessageWithMeta(ctx context.Context, conversationID, role, content string, meta *contracts.MessageMeta) (contracts.Message, error) {
	parent := ""
	if t, err := r.tree(ctx, conversationID); err == nil {
		if path := t.shownPath(); len(path) > 0 {
			parent = path[len(path)-1].ID
		}
	}
	return r.AddReply(ctx, conversationID, parent, role, content, meta)
}

// AddReply adds a message after parentID ("" for the chat's first point),
// as a new version there when it already has one, and shows it.
func (r *ConversationRepo) AddReply(ctx context.Context, conversationID, parentID, role, content string, meta *contracts.MessageMeta) (contracts.Message, error) {
	now := time.Now().UTC()
	m := contracts.Message{
		ID:             uuid.NewString(),
		ConversationID: conversationID,
		Role:           role,
		Content:        content,
		CreatedAt:      now,
		Meta:           meta,
		ParentID:       parentID,
	}
	var metaJSON any
	if meta != nil {
		b, _ := json.Marshal(meta)
		metaJSON = string(b)
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO messages (id, conversation_id, role, content, meta_json, created_at, parent_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.ConversationID, m.Role, m.Content, metaJSON, m.CreatedAt.Format(time.RFC3339Nano), parentID)
	if err != nil {
		return m, err
	}
	if err := r.setShown(ctx, conversationID, parentID, m.ID); err != nil {
		return m, err
	}
	_, _ = r.db.ExecContext(ctx, `UPDATE conversations SET updated_at = ? WHERE id = ?`, now.Format(time.RFC3339Nano), conversationID)
	return m, nil
}

// UpdateMessageMeta changes a saved message's metadata, such as an
// answer's links checked after it was saved (#209).
func (r *ConversationRepo) UpdateMessageMeta(ctx context.Context, conversationID, messageID string, change func(*contracts.MessageMeta)) error {
	var raw sql.NullString
	if err := r.db.QueryRowContext(ctx, `SELECT meta_json FROM messages WHERE id = ? AND conversation_id = ?`, messageID, conversationID).Scan(&raw); err != nil {
		return err
	}
	meta := &contracts.MessageMeta{}
	if raw.Valid && raw.String != "" {
		if err := json.Unmarshal([]byte(raw.String), meta); err != nil {
			return err
		}
	}
	change(meta)
	b, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `UPDATE messages SET meta_json = ? WHERE id = ? AND conversation_id = ?`, string(b), messageID, conversationID)
	return err
}
