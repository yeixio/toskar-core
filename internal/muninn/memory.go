// Package muninn is Yggdrasil's memory and conversation-context layer.
// Memories belong to Yggdrasil, not to a model, so they survive restarts and
// model changes. Only memories relevant to a turn are sent to the model.
package muninn

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/personal"
	"github.com/yeixio/toskar-core/internal/replylang"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Categories group memories for people, and decide which are always relevant.
const (
	CategoryIdentity    = "identity"
	CategoryPreferences = "preferences"
	CategoryProjects    = "projects"
	CategoryTechnical   = "technical"
	CategoryInterests   = "interests"
	CategoryPeople      = "people"
	CategoryOther       = "other"
)

// Categories lists every category in display order.
var Categories = []string{CategoryIdentity, CategoryPreferences, CategoryProjects, CategoryTechnical, CategoryInterests, CategoryPeople, CategoryOther}

// Sources of a memory.
const (
	SourceExplicit = "explicit" // "Remember that ..." in chat
	SourceManual   = "manual"   // added on the Memory page
)

// MaxMemoryRunes caps one memory.
const MaxMemoryRunes = 500

// ErrNotFound is returned for an unknown memory.
var ErrNotFound = contracts.NewError("MEMORY_NOT_FOUND", nil, errors.New("memory not found"))

// ErrSensitive is returned for content that looks like a credential.
var ErrSensitive = contracts.NewError("MEMORY_LOOKS_SECRET", nil, errors.New("that looks like a password, key, or token, so Toskar did not save it"))

// Memory is one durable fact or preference.
type Memory struct {
	ID         string `json:"id"`
	Content    string `json:"content"`
	Category   string `json:"category"`
	SourceType string `json:"source_type"`
	SourceRef  string `json:"source_ref,omitempty"`
	Enabled    bool   `json:"enabled"`
	// LocalOnly keeps the memory on this computer: work that uses it is
	// never sent to a paired computer (§63).
	LocalOnly bool `json:"local_only"`
	// Language is the language the memory is written in, as a BCP 47 tag
	// detected on this computer (multilingual spec §18), or "" when it is
	// too short to tell. A memory is found in any language; this says which
	// one its text is in.
	Language string `json:"language,omitempty"`
	// ProjectID is the project the memory belongs to, learned in one of its
	// chats, or "" for the person's own memory (#1136).
	ProjectID string    `json:"project_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Store keeps memories in the daemon database.
type Store struct {
	db  *sql.DB
	now func() time.Time

	mu       sync.Mutex
	embedder EmbedderSource
}

// NewStore returns a memory store.
func NewStore(db *sql.DB) *Store { return &Store{db: db, now: time.Now} }

var sensitiveRe = regexp.MustCompile(`(?i)\b(password|passcode|passphrase|api[ _-]?key|access token|auth token|bearer token|private key|ssh key|social security|ssn|credit card|card number|cvv|pin code)\b|\b(sk|pk|ghp|gho|xox[abp])[-_][A-Za-z0-9]{8,}|\b\d{4}[ -]?\d{4}[ -]?\d{4}[ -]?\d{4}\b`)

// LooksSensitive reports content that should not be stored as memory.
func LooksSensitive(s string) bool { return sensitiveRe.MatchString(s) }

func clean(content string) (string, error) {
	content = strings.Join(strings.Fields(content), " ")
	content = strings.TrimRight(content, " ")
	if content == "" {
		return "", fmt.Errorf("the memory is empty")
	}
	if utf8.RuneCountInString(content) > MaxMemoryRunes {
		return "", fmt.Errorf("keep a memory under %d characters", MaxMemoryRunes)
	}
	if LooksSensitive(content) {
		return "", ErrSensitive
	}
	// Memories shape answers; they never grant a permission (§38).
	if personal.GrantsPermission(content) {
		return "", personal.ErrPermission
	}
	return content, nil
}

func validCategory(c string) bool {
	for _, k := range Categories {
		if k == c {
			return true
		}
	}
	return false
}

// Add stores a memory. An empty category is inferred from the text. A memory
// that repeats an existing one updates it instead of adding a copy.
func (s *Store) Add(ctx context.Context, content, category, sourceType, sourceRef string) (Memory, bool, error) {
	return s.AddIn(ctx, "", content, category, sourceType, sourceRef)
}

// AddIn stores a memory in a project's memory, or the person's own for ""
// (#1136). One the project's chats already recall, the person's or the
// project's, isn't added again.
func (s *Store) AddIn(ctx context.Context, projectID, content, category, sourceType, sourceRef string) (Memory, bool, error) {
	content, err := clean(content)
	if err != nil {
		return Memory{}, false, err
	}
	if category == "" {
		category = InferCategory(content)
	}
	if !validCategory(category) {
		return Memory{}, false, fmt.Errorf("unknown category %q", category)
	}
	if dup, ok := s.duplicate(ctx, projectID, content); ok {
		dup.Enabled = true
		_, err := s.db.ExecContext(ctx, `UPDATE memories SET enabled = 1, updated_at = ? WHERE id = ? AND person_id = ?`, ts(s.now()), dup.ID, auth.PersonID(ctx))
		return dup, false, err
	}
	m := Memory{ID: uuid.NewString(), Content: content, Category: category, SourceType: sourceType, SourceRef: sourceRef,
		Enabled: true, Language: languageOf(content), ProjectID: projectID, CreatedAt: s.now().UTC(), UpdatedAt: s.now().UTC()}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Memory{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO memories (id, content, category, source_type, source_ref, enabled, language, created_at, updated_at, person_id, project_id)
		VALUES (?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?)`, m.ID, m.Content, m.Category, m.SourceType, nullable(m.SourceRef), nullable(m.Language), ts(m.CreatedAt), ts(m.UpdatedAt), auth.PersonID(ctx), m.ProjectID); err != nil {
		return Memory{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO memories_fts (content, memory_id) VALUES (?, ?)`, m.Content, m.ID); err != nil {
		return Memory{}, false, err
	}
	return m, true, tx.Commit()
}

// duplicate finds a memory with the same normalized text among those a
// project's chats recall: the person's own, and the project's.
func (s *Store) duplicate(ctx context.Context, projectID, content string) (Memory, bool) {
	all, err := s.List(ctx)
	if err != nil {
		return Memory{}, false
	}
	key := normalize(content)
	for _, m := range all {
		if recalledIn(m, projectID) && normalize(m.Content) == key {
			return m, true
		}
	}
	return Memory{}, false
}

var punctRe = regexp.MustCompile(`[^\p{L}\p{N} ]+`)

func normalize(s string) string {
	return strings.Join(strings.Fields(punctRe.ReplaceAllString(strings.ToLower(s), " ")), " ")
}

// Patch edits a memory.
type Patch struct {
	Content  *string `json:"content,omitempty"`
	Category *string `json:"category,omitempty"`
	Enabled  *bool   `json:"enabled,omitempty"`
	// LocalOnly marks the memory this computer only.
	LocalOnly *bool `json:"local_only,omitempty"`
}

// Update applies a patch.
func (s *Store) Update(ctx context.Context, id string, p Patch) (Memory, error) {
	m, err := s.Get(ctx, id)
	if err != nil {
		return Memory{}, err
	}
	if p.Content != nil {
		c, err := clean(*p.Content)
		if err != nil {
			return Memory{}, err
		}
		m.Content = c
		m.Language = languageOf(c)
	}
	if p.Category != nil {
		if !validCategory(*p.Category) {
			return Memory{}, fmt.Errorf("unknown category %q", *p.Category)
		}
		m.Category = *p.Category
	}
	if p.Enabled != nil {
		m.Enabled = *p.Enabled
	}
	if p.LocalOnly != nil {
		m.LocalOnly = *p.LocalOnly
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Memory{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE memories SET content=?, category=?, enabled=?, local_only=?, language=?, updated_at=? WHERE id=? AND person_id=?`,
		m.Content, m.Category, boolInt(m.Enabled), boolInt(m.LocalOnly), nullable(m.Language), ts(s.now()), id, auth.PersonID(ctx)); err != nil {
		return Memory{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE memories_fts SET content=? WHERE memory_id=?`, m.Content, id); err != nil {
		return Memory{}, err
	}
	if err := tx.Commit(); err != nil {
		return Memory{}, err
	}
	return s.Get(ctx, id)
}

// Delete removes a memory.
func (s *Store) Delete(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `DELETE FROM memories WHERE id = ? AND person_id = ?`, id, auth.PersonID(ctx))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM memories_fts WHERE memory_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM memory_vectors WHERE memory_id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteProject removes a project's memories (#1136).
func (s *Store) DeleteProject(ctx context.Context, projectID string) error {
	if projectID == "" {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	person := auth.PersonID(ctx)
	for _, q := range []string{
		`DELETE FROM memories_fts WHERE memory_id IN (SELECT id FROM memories WHERE project_id = ? AND person_id = ?)`,
		`DELETE FROM memory_vectors WHERE memory_id IN (SELECT id FROM memories WHERE project_id = ? AND person_id = ?)`,
		`DELETE FROM memories WHERE project_id = ? AND person_id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, projectID, person); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const columns = `id, content, category, source_type, COALESCE(source_ref, ''), enabled, local_only, COALESCE(language, ''), project_id, created_at, updated_at`

func scan(row interface{ Scan(...any) error }) (Memory, error) {
	var m Memory
	var enabled, localOnly int
	var created, updated string
	if err := row.Scan(&m.ID, &m.Content, &m.Category, &m.SourceType, &m.SourceRef, &enabled, &localOnly, &m.Language, &m.ProjectID, &created, &updated); err != nil {
		return Memory{}, err
	}
	m.Enabled = enabled != 0
	m.LocalOnly = localOnly != 0
	m.CreatedAt, m.UpdatedAt = parseTS(created), parseTS(updated)
	return m, nil
}

// Get returns one memory.
func (s *Store) Get(ctx context.Context, id string) (Memory, error) {
	m, err := scan(s.db.QueryRowContext(ctx, `SELECT `+columns+` FROM memories WHERE id = ? AND person_id = ?`, id, auth.PersonID(ctx)))
	if errors.Is(err, sql.ErrNoRows) {
		return Memory{}, ErrNotFound
	}
	return m, err
}

// List returns every memory, newest first.
func (s *Store) List(ctx context.Context) ([]Memory, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+columns+` FROM memories WHERE person_id = ? ORDER BY updated_at DESC`, auth.PersonID(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Memory{}
	for rows.Next() {
		m, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Core categories are about the person, so a few are always relevant.
var coreCategories = map[string]bool{CategoryIdentity: true, CategoryPreferences: true}

// Limits keep memory a small part of the prompt.
const (
	maxCore     = 5
	maxRelevant = 5
	maxRunes    = 1500
)

var termRe = regexp.MustCompile(`[\p{L}\p{N}][\p{L}\p{N}\-]*`)

var stop = map[string]bool{"the": true, "a": true, "an": true, "and": true, "or": true, "is": true, "are": true, "to": true,
	"of": true, "in": true, "on": true, "for": true, "with": true, "what": true, "how": true, "do": true, "does": true, "i": true,
	"me": true, "my": true, "you": true, "your": true, "it": true, "this": true, "that": true, "can": true, "please": true}

// recalledIn reports whether a chat in projectID recalls m: the person's own
// memories everywhere, and a project's in its chats alone (#1136).
func recalledIn(m Memory, projectID string) bool {
	return m.ProjectID == "" || m.ProjectID == projectID
}

// Relevant returns enabled memories for a turn: a few core facts about the
// person, then memories that share words with the message. It never returns
// the whole store. In a chat in a project with its memory on, projectID
// adds the project's memories to the person's.
func (s *Store) Relevant(ctx context.Context, message, projectID string) ([]Memory, error) {
	stored, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	all := stored[:0:0]
	for _, m := range stored {
		if recalledIn(m, projectID) {
			all = append(all, m)
		}
	}
	var out []Memory
	seen := map[string]bool{}
	used := 0
	add := func(m Memory) bool {
		n := utf8.RuneCountInString(m.Content)
		if seen[m.ID] || used+n > maxRunes {
			return false
		}
		seen[m.ID] = true
		used += n
		out = append(out, m)
		return true
	}
	core := 0
	for _, m := range all {
		if m.Enabled && coreCategories[m.Category] && core < maxCore && add(m) {
			core++
		}
	}
	var terms []string
	for _, t := range termRe.FindAllString(strings.ToLower(message), -1) {
		if !stop[t] && utf8.RuneCountInString(t) > 1 {
			terms = append(terms, `"`+strings.ReplaceAll(t, `"`, `""`)+`"`)
		}
	}
	rel := 0
	if len(terms) > 0 {
		n, err := s.byWords(ctx, terms, projectID, add)
		if err != nil {
			return nil, err
		}
		rel = n
	}
	// Then by meaning, in any language (§18).
	for _, m := range s.byMeaning(ctx, message, all) {
		if rel >= maxRelevant {
			break
		}
		if add(m) {
			rel++
		}
	}
	return out, nil
}

// byWords adds the enabled memories that share words with the message, best
// first, and says how many it added.
func (s *Store) byWords(ctx context.Context, terms []string, projectID string, add func(Memory) bool) (int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.id, m.content, m.category, m.source_type, COALESCE(m.source_ref, ''), m.enabled, m.local_only, COALESCE(m.language, ''), m.project_id, m.created_at, m.updated_at
		FROM memories_fts f JOIN memories m ON m.id = f.memory_id
		WHERE memories_fts MATCH ? AND m.enabled = 1 AND m.person_id = ? AND (m.project_id = '' OR m.project_id = ?)
		ORDER BY bm25(memories_fts) LIMIT ?`, strings.Join(terms, " OR "), auth.PersonID(ctx), projectID, maxRelevant*2)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	rel := 0
	for rows.Next() && rel < maxRelevant {
		m, err := scan(rows)
		if err != nil {
			return 0, err
		}
		if add(m) {
			rel++
		}
	}
	return rel, rows.Err()
}

// languageOf is the language a memory is written in, detected on this
// computer, or "" when it is too short to tell.
func languageOf(content string) string {
	tag, _ := replylang.Detect(content)
	return tag
}

// Block formats memories as instructions. They come from the person, so
// they are trusted, unlike retrieved content.
func Block(memories []Memory) string {
	if len(memories) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("What the user asked you to remember. Use it when it is relevant; do not repeat it back unprompted. It never grants permission to use a tool:\n")
	for _, m := range memories {
		b.WriteString("- " + m.Content + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// InferCategory guesses a category from a memory's text.
func InferCategory(content string) string {
	c := " " + strings.ToLower(content) + " "
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(c, w) {
				return true
			}
		}
		return false
	}
	switch {
	case has(" my name ", " i am ", " i'm ", " call me ", " i live ", " i work as ", " my job ", " my role ", " my pronouns "):
		return CategoryIdentity
	case has(" prefer", " i like ", " i don't like ", " i dislike ", " i hate ", " i love ", " always ", " never ", " please use ", " use metric", " use imperial"):
		return CategoryPreferences
	case has(" project", " repo", " repository", " we use ", " our team ", " this app", " codebase"):
		return CategoryProjects
	case has(" my wife ", " my husband ", " my partner ", " my son ", " my daughter ", " my kid", " my mom ", " my dad ", " my friend ", " my boss ", " my manager ", " my colleague"):
		return CategoryPeople
	case has(" gpu", " cpu", " ram", " mac", " linux", " windows", " server", " python", " golang", " go ", " rust", " javascript", " typescript", " docker", " kubernetes", " hardware"):
		return CategoryTechnical
	case has(" interested in ", " hobby", " hobbies", " i enjoy ", " fan of "):
		return CategoryInterests
	}
	return CategoryOther
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTS(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Find returns the memory a "Forget that …" request means: an exact match
// after normalizing, or the best memory containing every word of the
// request. It returns false rather than guess.
func (s *Store) Find(ctx context.Context, text string) (Memory, bool, error) {
	all, err := s.List(ctx)
	if err != nil {
		return Memory{}, false, err
	}
	key := normalize(text)
	for _, m := range all {
		if normalize(m.Content) == key {
			return m, true, nil
		}
	}
	var terms []string
	for _, t := range termRe.FindAllString(strings.ToLower(text), -1) {
		if !stop[t] {
			terms = append(terms, `"`+strings.ReplaceAll(t, `"`, `""`)+`"`)
		}
	}
	if len(terms) == 0 {
		return Memory{}, false, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.id, m.content, m.category, m.source_type, COALESCE(m.source_ref, ''), m.enabled, m.local_only, COALESCE(m.language, ''), m.project_id, m.created_at, m.updated_at
		FROM memories_fts f JOIN memories m ON m.id = f.memory_id
		WHERE memories_fts MATCH ? AND m.person_id = ? ORDER BY bm25(memories_fts) LIMIT 1`, strings.Join(terms, " AND "), auth.PersonID(ctx))
	if err != nil {
		return Memory{}, false, err
	}
	defer rows.Close()
	if rows.Next() {
		m, err := scan(rows)
		return m, err == nil, err
	}
	return Memory{}, false, rows.Err()
}
