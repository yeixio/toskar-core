// Package mimir is Yggdrasil's connected-knowledge layer. It keeps current,
// authoritative information (catalogs, prices, policies, documents) outside
// model weights and retrieves it for a chat turn. Refreshing a source rebuilds
// its index; no model is retrained.
package mimir

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Kind is how a source's content reaches Mimir.
type Kind string

const (
	// KindPath reads a file or folder on this computer. It refreshes itself
	// when the files change.
	KindPath Kind = "path"
	// KindText is content the user pasted or uploaded. Mimir keeps a copy.
	KindText Kind = "text"
)

// Status of a source's index.
const (
	StatusReady    = "ready"
	StatusFailed   = "failed"
	StatusIndexing = "indexing"
)

// ErrNotFound is returned for an unknown source id.
var ErrNotFound = errors.New("knowledge source not found")

// Source is one connected knowledge source.
type Source struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Kind       Kind   `json:"kind"`
	Path       string `json:"path,omitempty"`
	Filename   string `json:"filename,omitempty"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
	ChunkCount int    `json:"chunk_count"`
	// EmbeddedCount is how many passages have a vector for semantic search,
	// and EmbeddingModel the model that made them. Both are empty until an
	// embedding model is installed.
	EmbeddedCount  int        `json:"embedded_count"`
	EmbeddingModel string     `json:"embedding_model,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	RefreshedAt    *time.Time `json:"refreshed_at,omitempty"`
	// LocalOnly keeps the source on this computer: a turn that uses its
	// passages is never sent to a paired computer (§63).
	LocalOnly bool `json:"local_only"`

	// file is where Mimir reads the source. For text sources it is the copy
	// Mimir keeps, which the API does not expose.
	file string
}

// CreateInput adds a source.
type CreateInput struct {
	Name string `json:"name"`
	Kind Kind   `json:"kind"`
	// Path is a file or folder for KindPath.
	Path string `json:"path,omitempty"`
	// Filename and Text carry uploaded or pasted content for KindText. The
	// extension of Filename picks the reader (for example .csv).
	Filename string `json:"filename,omitempty"`
	Text     string `json:"text,omitempty"`
	// ContentBase64 carries binary uploads such as .xlsx, in place of Text.
	ContentBase64 string `json:"content_base64,omitempty"`
}

// uploadBytes returns an upload's content, decoding base64 when set.
func (in CreateInput) uploadBytes() ([]byte, error) {
	if in.ContentBase64 == "" {
		return []byte(in.Text), nil
	}
	if base64.StdEncoding.DecodedLen(len(in.ContentBase64)) > MaxTextBytes {
		return nil, fmt.Errorf("the file is larger than %d MB; connect it from disk instead", MaxTextBytes>>20)
	}
	b, err := base64.StdEncoding.DecodeString(in.ContentBase64)
	if err != nil {
		return nil, fmt.Errorf("the upload is not valid base64")
	}
	return b, nil
}

// Store indexes and searches knowledge sources.
type Store struct {
	db  *sql.DB
	dir string
	now func() time.Time

	// refreshMu serializes rebuilds so two chats do not index one folder twice.
	refreshMu sync.Mutex

	// semMu guards the supporting models and the background indexer.
	semMu  sync.Mutex
	models Models
	kick   chan struct{}

	// recognizer reads scanned PDF pages, when text recognition is set up.
	recognizer Recognizer
}

// MaxTextBytes limits pasted or uploaded content.
const MaxTextBytes = 20 << 20

// NewStore opens Mimir over the daemon database. dir holds copies of uploaded
// and pasted content.
func NewStore(db *sql.DB, dir string) *Store {
	return &Store{db: db, dir: dir, now: time.Now}
}

// Create adds and indexes a source.
func (s *Store) Create(ctx context.Context, in CreateInput) (Source, error) {
	in.Name = strings.TrimSpace(in.Name)
	switch in.Kind {
	case KindPath:
		in.Path = strings.TrimSpace(in.Path)
		if in.Path == "" {
			return Source{}, fmt.Errorf("choose a file or folder")
		}
		abs, err := filepath.Abs(expandHome(in.Path))
		if err != nil {
			return Source{}, err
		}
		if _, err := os.Stat(abs); err != nil {
			return Source{}, fmt.Errorf("cannot read %s: %w", in.Path, err)
		}
		in.Path = abs
		if in.Name == "" {
			in.Name = filepath.Base(abs)
		}
	case KindText:
		if strings.TrimSpace(in.Text) == "" && in.ContentBase64 == "" {
			return Source{}, fmt.Errorf("the content is empty")
		}
		if len(in.Text) > MaxTextBytes {
			return Source{}, fmt.Errorf("the content is larger than %d MB; connect the file or folder instead", MaxTextBytes>>20)
		}
		in.Filename = filepath.Base(strings.TrimSpace(in.Filename))
		if in.Filename == "" || in.Filename == "." {
			in.Filename = "pasted.txt"
		}
		if in.Name == "" {
			in.Name = in.Filename
		}
	default:
		return Source{}, fmt.Errorf("kind must be path or text")
	}

	id := uuid.NewString()
	now := s.now().UTC()
	path := in.Path
	if in.Kind == KindText {
		path = filepath.Join(s.dir, id, in.Filename)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return Source{}, err
		}
		content, err := in.uploadBytes()
		if err != nil {
			return Source{}, err
		}
		if len(content) == 0 {
			return Source{}, fmt.Errorf("the content is empty")
		}
		if err := os.WriteFile(path, content, 0o600); err != nil {
			return Source{}, err
		}
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO knowledge_sources (id, name, kind, path, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, in.Name, string(in.Kind), path, StatusIndexing, fmtTime(now), fmtTime(now)); err != nil {
		return Source{}, err
	}
	if err := s.Refresh(ctx, id); err != nil {
		// The row stays with status failed so the user can see why and retry.
		src, getErr := s.Get(ctx, id)
		if getErr != nil {
			return Source{}, err
		}
		return src, nil
	}
	return s.Get(ctx, id)
}

// UpdateInput changes a source's name, or replaces the content of a text source.
type UpdateInput struct {
	Name *string `json:"name,omitempty"`
	Text *string `json:"text,omitempty"`
	// LocalOnly marks the source this computer only.
	LocalOnly *bool `json:"local_only,omitempty"`
}

// Update renames a source or replaces a text source's content and reindexes it.
func (s *Store) Update(ctx context.Context, id string, in UpdateInput) (Source, error) {
	src, err := s.Get(ctx, id)
	if err != nil {
		return Source{}, err
	}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return Source{}, fmt.Errorf("name is required")
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE knowledge_sources SET name=?, updated_at=? WHERE id=?`,
			name, fmtTime(s.now().UTC()), id); err != nil {
			return Source{}, err
		}
	}
	if in.LocalOnly != nil {
		local := 0
		if *in.LocalOnly {
			local = 1
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE knowledge_sources SET local_only=?, updated_at=? WHERE id=?`,
			local, fmtTime(s.now().UTC()), id); err != nil {
			return Source{}, err
		}
	}
	if in.Text != nil {
		if src.Kind != KindText {
			return Source{}, fmt.Errorf("edit the files on disk; this source refreshes from %s", src.Path)
		}
		if !Editable(src.Filename) {
			return Source{}, fmt.Errorf("%s is not a text file; upload a new copy to change it", src.Filename)
		}
		if len(*in.Text) > MaxTextBytes {
			return Source{}, fmt.Errorf("the content is larger than %d MB", MaxTextBytes>>20)
		}
		if err := os.WriteFile(src.file, []byte(*in.Text), 0o600); err != nil {
			return Source{}, err
		}
		if err := s.Refresh(ctx, id); err != nil {
			return s.Get(ctx, id)
		}
	}
	return s.Get(ctx, id)
}

// Content returns the copy Mimir keeps of a pasted or uploaded source.
func (s *Store) Content(ctx context.Context, id string) (string, error) {
	src, err := s.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if src.Kind != KindText {
		return "", fmt.Errorf("this source is read from %s; edit the files there", src.Path)
	}
	if !Editable(src.Filename) {
		return "", fmt.Errorf("%s is not a text file; upload a new copy to change it", src.Filename)
	}
	b, err := os.ReadFile(src.file)
	return string(b), err
}

// Delete removes a source, its index, and any copy Mimir kept.
func (s *Store) Delete(ctx context.Context, id string) error {
	src, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_fts WHERE source_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_vectors WHERE source_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_sources WHERE id = ?`, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if src.Kind == KindText {
		_ = os.RemoveAll(filepath.Join(s.dir, id))
	}
	return nil
}

// Get returns one source.
func (s *Store) Get(ctx context.Context, id string) (Source, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT `+sourceColumns+`
		FROM knowledge_sources WHERE id = ?`, id)
	src, err := scanSource(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Source{}, ErrNotFound
	}
	return src, err
}

// List returns every source, newest first.
func (s *Store) List(ctx context.Context) ([]Source, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+sourceColumns+`
		FROM knowledge_sources ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Source{}
	for rows.Next() {
		src, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

// Refresh rebuilds a source's index from its files.
func (s *Store) Refresh(ctx context.Context, id string) error {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	return s.refreshLocked(ctx, id)
}

func (s *Store) refreshLocked(ctx context.Context, id string) error {
	src, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	sig, sigErr := signature(src.file)
	docs, readErr := readSource(src.file, s.readPDF(ctx))
	if sigErr != nil || readErr != nil {
		msg := errors.Join(sigErr, readErr).Error()
		_, _ = s.db.ExecContext(ctx, `UPDATE knowledge_sources SET status=?, error=?, updated_at=? WHERE id=?`,
			StatusFailed, msg, fmtTime(s.now().UTC()), id)
		return errors.Join(sigErr, readErr)
	}
	chunks := chunkDocuments(docs)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Passages whose text did not change keep their vectors, so editing one
	// price does not re-embed the whole catalog.
	kept, err := keepVectors(ctx, tx, id)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_fts WHERE source_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_vectors WHERE source_id = ?`, id); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO knowledge_fts (title, body, source_id, ordinal) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for i, c := range chunks {
		res, err := stmt.ExecContext(ctx, c.Title, c.Body, id, i)
		if err != nil {
			return err
		}
		if len(kept) == 0 {
			continue
		}
		hash := textHash(embedText(c.Title, c.Body))
		v, ok := kept[hash]
		if !ok {
			continue
		}
		rowid, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO knowledge_vectors (chunk_rowid, source_id, model, hash, vec) VALUES (?, ?, ?, ?, ?)`,
			rowid, id, v.model, hash, v.vec); err != nil {
			return err
		}
	}
	now := fmtTime(s.now().UTC())
	if _, err := tx.ExecContext(ctx, `
		UPDATE knowledge_sources SET status=?, error=NULL, chunk_count=?, signature=?, refreshed_at=?, updated_at=?
		WHERE id=?`, StatusReady, len(chunks), sig, now, now, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.Kick()
	return nil
}

// refreshIfChanged reindexes sources whose files changed since the last index.
func (s *Store) refreshIfChanged(ctx context.Context, ids []string) {
	for _, id := range ids {
		var path string
		var stored sql.NullString
		if err := s.db.QueryRowContext(ctx, `SELECT path, signature FROM knowledge_sources WHERE id=?`, id).
			Scan(&path, &stored); err != nil {
			continue
		}
		current, err := signature(path)
		if err != nil || current == stored.String {
			continue
		}
		s.refreshMu.Lock()
		_ = s.refreshLocked(ctx, id)
		s.refreshMu.Unlock()
	}
}

type scanner interface{ Scan(...any) error }

// sourceColumns are the columns scanSource reads. The vector count is per
// source, from the model most of its vectors came from.
const sourceColumns = `id, name, kind, path, status, error, chunk_count, created_at, updated_at, refreshed_at, local_only,
	(SELECT COUNT(*) FROM knowledge_vectors v WHERE v.source_id = knowledge_sources.id
	 AND v.model = (SELECT model FROM knowledge_vectors w WHERE w.source_id = knowledge_sources.id GROUP BY model ORDER BY COUNT(*) DESC LIMIT 1)),
	(SELECT model FROM knowledge_vectors w WHERE w.source_id = knowledge_sources.id GROUP BY model ORDER BY COUNT(*) DESC LIMIT 1)`

func scanSource(row scanner) (Source, error) {
	var src Source
	var kind, created, updated string
	var path, errText, refreshed, embModel sql.NullString
	var localOnly int
	if err := row.Scan(&src.ID, &src.Name, &kind, &path, &src.Status, &errText, &src.ChunkCount, &created, &updated, &refreshed, &localOnly,
		&src.EmbeddedCount, &embModel); err != nil {
		return Source{}, err
	}
	src.LocalOnly = localOnly != 0
	src.EmbeddingModel = embModel.String
	src.Kind = Kind(kind)
	src.Path = path.String
	src.file = path.String
	if src.Kind == KindText {
		src.Filename = filepath.Base(src.Path)
		src.Path = ""
	}
	src.Error = errText.String
	src.CreatedAt = parseTime(created)
	src.UpdatedAt = parseTime(updated)
	if refreshed.Valid {
		t := parseTime(refreshed.String)
		src.RefreshedAt = &t
	}
	return src, nil
}

func fmtTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}
