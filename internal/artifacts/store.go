// Package artifacts keeps the files people attach to chats and the files the
// assistant produces, in one store (AI experience spec §28). The bytes live
// under the data directory; SQLite holds the index.
package artifacts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Producers.
const (
	ProducerUser      = "user"
	ProducerAssistant = "assistant"
)

// MaxBytes caps one file.
const MaxBytes = 25 << 20

// ErrNotFound is returned for an unknown artifact.
var ErrNotFound = contracts.NewError("FILE_NOT_FOUND", nil, errors.New("file not found"))

// Artifact is one stored file.
type Artifact struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id,omitempty"`
	Name           string `json:"name"`
	MimeType       string `json:"mime_type"`
	// Kind is document, spreadsheet, pdf, image, code, or other.
	Kind       string    `json:"kind"`
	Size       int64     `json:"size_bytes"`
	Producer   string    `json:"producer"`
	SourceTask string    `json:"source_task,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	// ExpiresAt is when a file that belongs to no chat is removed (#191).
	// A file in a chat has none and stays with the chat.
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	// Details are how it was made, such as a picture's model, seed, and
	// prompt (#553).
	Details map[string]any `json:"details,omitempty"`
	path    string
}

// UnfiledTTL is how long a file that belongs to no chat is kept: one an
// automation run, an API call, or an MCP client made, or an upload that was
// never sent. A week leaves time to download it.
const UnfiledTTL = 7 * 24 * time.Hour

// Input is a file to save.
type Input struct {
	ConversationID string
	Name           string
	Producer       string
	SourceTask     string
	Data           []byte
	// Details are how it was made, kept with it (#553).
	Details map[string]any
}

// Store saves and finds artifacts.
type Store struct {
	db  *sql.DB
	dir string
	now func() time.Time
	// KeepFor, when set, says how long an unfiled file from source is kept
	// instead of UnfiledTTL, and ok false leaves UnfiledTTL. A zero
	// duration keeps it. An automation's files are kept as long as its
	// runs (#1209).
	KeepFor func(ctx context.Context, source string) (keep time.Duration, ok bool)
}

// NewStore keeps files under dir.
func NewStore(db *sql.DB, dir string) *Store {
	return &Store{db: db, dir: dir, now: time.Now}
}

// CleanName keeps a file name safe to store and to offer as a download: no
// directories, no control characters, and a sensible length.
func CleanName(name string) string {
	name = filepath.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, ". ")
	if name == "" {
		name = "file"
	}
	if r := []rune(name); len(r) > 120 {
		ext := filepath.Ext(name)
		name = string(r[:120-len([]rune(ext))]) + ext
	}
	return name
}

// MimeType guesses a file's type from its name.
func MimeType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".md", ".markdown":
		return "text/markdown; charset=utf-8"
	case ".csv":
		return "text/csv; charset=utf-8"
	case ".tsv":
		return "text/tab-separated-values; charset=utf-8"
	case ".jsonl":
		return "application/x-ndjson"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".wav":
		return "audio/wav"
	case ".mp3":
		return "audio/mpeg"
	case ".m4a", ".aac":
		return "audio/mp4"
	case ".ogg":
		return "audio/ogg"
	case ".flac":
		return "audio/flac"
	case ".webm":
		// A clip, or a voice recording from a browser; either plays as video.
		return "video/webm"
	}
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	if kindOf(name) == "code" {
		return "text/plain; charset=utf-8"
	}
	return "application/octet-stream"
}

// IsAudio reports an audio file: one that can be played and transcribed.
// A .webm file counts: it is a video kind, but its sound can be transcribed.
func IsAudio(name string) bool {
	return kindOf(name) == "audio" || strings.EqualFold(filepath.Ext(name), ".webm")
}

// IsVideo reports a video file, whose frames a model that can see is shown.
func IsVideo(name string) bool { return kindOf(name) == "video" }

func kindOf(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".csv", ".tsv", ".xlsx":
		return "spreadsheet"
	case ".pdf":
		return "pdf"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return "image"
	case ".wav", ".mp3", ".m4a", ".aac", ".ogg", ".flac":
		return "audio"
	case ".webm", ".mp4", ".mov", ".m4v", ".mkv":
		return "video"
	case ".txt", ".md", ".markdown", ".html", ".htm", ".json", ".jsonl", ".log", ".docx", ".pptx":
		return "document"
	case ".py", ".js", ".ts", ".tsx", ".jsx", ".go", ".rs", ".java", ".kt", ".c", ".h", ".cpp", ".hpp", ".cs",
		".rb", ".php", ".swift", ".sh", ".sql", ".yaml", ".yml", ".toml", ".xml", ".css", ".ini":
		return "code"
	}
	return "other"
}

// Save stores a file and returns its record.
func (s *Store) Save(ctx context.Context, in Input) (Artifact, error) {
	if len(in.Data) > MaxBytes {
		return Artifact{}, fmt.Errorf("%s is larger than %d MB", in.Name, MaxBytes>>20)
	}
	name := CleanName(in.Name)
	producer := in.Producer
	if producer != ProducerAssistant {
		producer = ProducerUser
	}
	a := Artifact{
		ID: uuid.NewString(), ConversationID: in.ConversationID, Name: name, MimeType: MimeType(name),
		Kind: kindOf(name), Size: int64(len(in.Data)), Producer: producer, SourceTask: in.SourceTask,
		CreatedAt: s.now().UTC(),
		Details:   in.Details,
	}
	if in.ConversationID == "" {
		ttl := UnfiledTTL
		if s.KeepFor != nil && in.SourceTask != "" {
			if keep, ok := s.KeepFor(ctx, in.SourceTask); ok {
				ttl = keep
			}
		}
		if ttl > 0 {
			exp := a.CreatedAt.Add(ttl)
			a.ExpiresAt = &exp
		}
	}
	folder := in.ConversationID
	if folder == "" {
		folder = "unfiled"
	}
	a.path = filepath.Join(folder, a.ID+filepath.Ext(name))
	full := filepath.Join(s.dir, a.path)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return Artifact{}, err
	}
	if err := os.WriteFile(full, in.Data, 0o600); err != nil {
		return Artifact{}, err
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO artifacts (id, conversation_id, name, mime_type, kind, size_bytes, producer, source_task, path, created_at, expires_at, person_id, details)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, nullable(a.ConversationID), a.Name, a.MimeType, a.Kind, a.Size, a.Producer, nullable(a.SourceTask), a.path,
		a.CreatedAt.Format(time.RFC3339Nano), stamp(a.ExpiresAt), auth.PersonID(ctx), detailsJSON(a.Details))
	if err != nil {
		_ = os.Remove(full)
		return Artifact{}, err
	}
	return a, nil
}

const columns = `id, COALESCE(conversation_id, ''), name, mime_type, kind, size_bytes, producer, COALESCE(source_task, ''), path, created_at, COALESCE(expires_at, ''), COALESCE(details, '')`

func scan(row interface{ Scan(...any) error }) (Artifact, error) {
	var a Artifact
	var created, expires, details string
	if err := row.Scan(&a.ID, &a.ConversationID, &a.Name, &a.MimeType, &a.Kind, &a.Size, &a.Producer, &a.SourceTask, &a.path, &created, &expires, &details); err != nil {
		return Artifact{}, err
	}
	if details != "" {
		_ = json.Unmarshal([]byte(details), &a.Details)
	}
	a.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if t, err := time.Parse(time.RFC3339Nano, expires); err == nil {
		a.ExpiresAt = &t
	}
	return a, nil
}

func stamp(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// Get returns one artifact's record.
func (s *Store) Get(ctx context.Context, id string) (Artifact, error) {
	a, err := scan(s.db.QueryRowContext(ctx, `SELECT `+columns+` FROM artifacts WHERE id = ? AND person_id = ?`, id, auth.PersonID(ctx)))
	if errors.Is(err, sql.ErrNoRows) {
		return Artifact{}, ErrNotFound
	}
	return a, err
}

// Read returns an artifact's bytes.
func (s *Store) Read(ctx context.Context, id string) (Artifact, []byte, error) {
	a, err := s.Get(ctx, id)
	if err != nil {
		return Artifact{}, nil, err
	}
	data, err := os.ReadFile(filepath.Join(s.dir, a.path))
	if errors.Is(err, os.ErrNotExist) {
		return Artifact{}, nil, ErrNotFound
	}
	return a, data, err
}

// List returns a conversation's artifacts, oldest first.
func (s *Store) List(ctx context.Context, conversationID string) ([]Artifact, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+columns+` FROM artifacts WHERE conversation_id = ? AND person_id = ? ORDER BY created_at`, conversationID, auth.PersonID(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Artifact{}
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Attach files an unfiled artifact under a conversation, such as a file
// uploaded before the chat it belongs to was created.
func (s *Store) Attach(ctx context.Context, id, conversationID string) error {
	// In a chat, it stays with the chat.
	_, err := s.db.ExecContext(ctx, `UPDATE artifacts SET conversation_id = ?, expires_at = NULL WHERE id = ? AND conversation_id IS NULL AND person_id = ?`, conversationID, id, auth.PersonID(ctx))
	return err
}

// RemoveExpired removes files whose time is up, with their records, and
// returns how many (#191).
func (s *Store) RemoveExpired(ctx context.Context) (int, error) {
	now := s.now().UTC().Format(time.RFC3339Nano)
	rows, err := s.db.QueryContext(ctx, `SELECT id, path FROM artifacts WHERE expires_at IS NOT NULL AND expires_at <= ?`, now)
	if err != nil {
		return 0, err
	}
	type file struct{ id, path string }
	var expired []file
	for rows.Next() {
		var f file
		if err := rows.Scan(&f.id, &f.path); err != nil {
			rows.Close()
			return 0, err
		}
		expired = append(expired, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, f := range expired {
		// The record goes first, and only while it still expires: a file
		// attached to a chat since it was listed is kept.
		res, err := s.db.ExecContext(ctx, `DELETE FROM artifacts WHERE id = ? AND expires_at IS NOT NULL`, f.id)
		if err != nil {
			return n, err
		}
		if k, _ := res.RowsAffected(); k == 0 {
			continue
		}
		n++
		if err := os.Remove(filepath.Join(s.dir, f.path)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return n, err
		}
	}
	return n, nil
}

// Delete removes one artifact and its file.
func (s *Store) Delete(ctx context.Context, id string) error {
	a, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM artifacts WHERE id = ? AND person_id = ?`, id, auth.PersonID(ctx)); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(s.dir, a.path)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// DeleteBySource removes every file a source made, such as an automation's
// camera pictures ("automation:<id>", #1335), whoever's they are: the
// caller has checked the source is the request's to clear. It says how many.
func (s *Store) DeleteBySource(ctx context.Context, source string) (int, error) {
	if source == "" {
		return 0, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, path FROM artifacts WHERE source_task = ?`, source)
	if err != nil {
		return 0, err
	}
	type file struct{ id, path string }
	var files []file
	for rows.Next() {
		var f file
		if err := rows.Scan(&f.id, &f.path); err != nil {
			rows.Close()
			return 0, err
		}
		files = append(files, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, f := range files {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM artifacts WHERE id = ?`, f.id); err != nil {
			return n, err
		}
		if err := os.Remove(filepath.Join(s.dir, f.path)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return n, err
		}
		n++
	}
	return n, nil
}

// DeleteConversation removes a conversation's files. Their rows go with the
// conversation.
func (s *Store) DeleteConversation(ctx context.Context, conversationID string) error {
	if conversationID == "" || strings.ContainsAny(conversationID, `/\`) || strings.Contains(conversationID, "..") {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM artifacts WHERE conversation_id = ? AND person_id = ?`, conversationID, auth.PersonID(ctx)); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(s.dir, conversationID))
}

// DeleteAll removes every file of the request's person, for "delete all
// chats" (#206): other people's stay.
func (s *Store) DeleteAll(ctx context.Context) error {
	person := auth.PersonID(ctx)
	rows, err := s.db.QueryContext(ctx, `SELECT path FROM artifacts WHERE person_id = ?`, person)
	if err != nil {
		return err
	}
	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			return err
		}
		paths = append(paths, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM artifacts WHERE person_id = ?`, person); err != nil {
		return err
	}
	for _, p := range paths {
		full := filepath.Join(s.dir, p)
		if err := os.Remove(full); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		// A chat's folder goes once it's empty.
		_ = os.Remove(filepath.Dir(full))
	}
	return nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

type ctxKey struct{}

// WithAttachments carries the files attached to a chat message.
func WithAttachments(ctx context.Context, ids []string) context.Context {
	if len(ids) == 0 {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, append([]string(nil), ids...))
}

// AttachmentsFrom returns the files attached to a chat message.
func AttachmentsFrom(ctx context.Context) []string {
	ids, _ := ctx.Value(ctxKey{}).([]string)
	return ids
}

// detailsJSON is details for the details column, or NULL.
func detailsJSON(d map[string]any) any {
	if len(d) == 0 {
		return nil
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return nil
	}
	return string(raw)
}
