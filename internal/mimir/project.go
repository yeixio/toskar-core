package mimir

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A project's files (#1136) are one text source with the project's id: a
// folder of copies Mimir keeps, indexed as any source is. It is made with
// the project's first file and removed with its last, so it is never an
// empty source that failed to index. It isn't in List, so the Knowledge page
// and profile pickers don't show it, and searching every source leaves it
// out; a project's chats name it.

type projectKey struct{}

// WithProject lets a chat in a project, or the project's own routes, reach
// its files' source. Without it, Get, Search, and everything that takes a
// source id treat a project's files as not found.
func WithProject(ctx context.Context, projectID string) context.Context {
	if projectID == "" {
		return ctx
	}
	return context.WithValue(ctx, projectKey{}, projectID)
}

func projectFrom(ctx context.Context) string {
	id, _ := ctx.Value(projectKey{}).(string)
	return id
}

// visible keeps the ids ctx may search: every source on the Knowledge page,
// and the files of the project ctx is in. Others are left out, as an
// unknown id is.
func (s *Store) visible(ctx context.Context, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM knowledge_sources WHERE id IN (`+marks(len(ids))+`) AND (project_id = '' OR project_id = ?)`,
		append(anySlice(ids), projectFrom(ctx))...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ok := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ok[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []string{}
	for _, id := range ids {
		if ok[id] {
			out = append(out, id)
			delete(ok, id)
		}
	}
	return out, nil
}

// ErrProjectFileNotFound is a file the project doesn't have.
var ErrProjectFileNotFound = contracts.NewError("PROJECT_FILE_NOT_FOUND", nil, errors.New("the project has no file by that name"))

// ProjectSourceID is the source holding a project's files, or "" when it
// has none.
func (s *Store) ProjectSourceID(ctx context.Context, projectID string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM knowledge_sources WHERE project_id = ? AND project_id != ''`, projectID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// projectFileName is name as a file in a project's folder, or an error for
// one Mimir can't read.
func projectFileName(name string) (string, error) {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" || name == "." || name == string(filepath.Separator) || strings.HasPrefix(name, ".") {
		return "", fmt.Errorf("name the file, such as notes.md")
	}
	if !supported(name) {
		return "", fmt.Errorf("%s is not a supported file type (%s)", name, strings.Join(SupportedExtensions, ", "))
	}
	return name, nil
}

// AddProjectFile adds a file to a project's knowledge, or replaces the one
// with its name, and indexes it. A file that can't be read is taken back
// out, and the error says why.
func (s *Store) AddProjectFile(ctx context.Context, projectID, name string, content []byte) error {
	ctx = WithProject(ctx, projectID)
	name, err := projectFileName(name)
	if err != nil {
		return err
	}
	if len(content) == 0 {
		return fmt.Errorf("the content is empty")
	}
	if len(content) > MaxTextBytes {
		return fmt.Errorf("the file is larger than %d MB", MaxTextBytes>>20)
	}
	id, err := s.ProjectSourceID(ctx, projectID)
	if err != nil {
		return err
	}
	made := false
	if id == "" {
		id, made = uuid.NewString(), true
		now := fmtTime(s.now().UTC())
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO knowledge_sources (id, name, kind, path, status, created_at, updated_at, project_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			id, "Project files", string(KindText), filepath.Join(s.dir, id), StatusIndexing, now, now, projectID); err != nil {
			return err
		}
	}
	dir := filepath.Join(s.dir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, name)
	previous, readErr := os.ReadFile(path)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return err
	}
	if err := s.Refresh(ctx, id); err != nil {
		// Put the folder back as it was, so one bad file doesn't stop the
		// others being searched.
		if readErr == nil {
			_ = os.WriteFile(path, previous, 0o600)
		} else {
			_ = os.Remove(path)
		}
		if made {
			_ = s.Delete(ctx, id)
		} else {
			_ = s.Refresh(ctx, id)
		}
		return err
	}
	return nil
}

// RemoveProjectFile takes a file out of a project's knowledge.
func (s *Store) RemoveProjectFile(ctx context.Context, projectID, name string) error {
	ctx = WithProject(ctx, projectID)
	id, err := s.ProjectSourceID(ctx, projectID)
	if err != nil {
		return err
	}
	name = filepath.Base(strings.TrimSpace(name))
	if id == "" || name == "" || name == "." || strings.HasPrefix(name, ".") {
		return ErrProjectFileNotFound
	}
	if err := os.Remove(filepath.Join(s.dir, id, name)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrProjectFileNotFound
		}
		return err
	}
	files, err := s.ProjectFiles(ctx, projectID)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return s.Delete(ctx, id)
	}
	return s.Refresh(ctx, id)
}

// ProjectFiles lists a project's files by name.
func (s *Store) ProjectFiles(ctx context.Context, projectID string) ([]contracts.ProjectFile, error) {
	out := []contracts.ProjectFile{}
	id, err := s.ProjectSourceID(ctx, projectID)
	if err != nil || id == "" {
		return out, err
	}
	entries, err := os.ReadDir(filepath.Join(s.dir, id))
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, contracts.ProjectFile{Name: e.Name(), Size: info.Size()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// DeleteProjectFiles removes a project's files and their source.
func (s *Store) DeleteProjectFiles(ctx context.Context, projectID string) error {
	ctx = WithProject(ctx, projectID)
	id, err := s.ProjectSourceID(ctx, projectID)
	if err != nil || id == "" {
		return err
	}
	return s.Delete(ctx, id)
}

// Linkable reports whether a source can be linked to a project: one on the
// Knowledge page, not another project's files.
func (s *Store) Linkable(ctx context.Context, id string) error {
	var projectID string
	err := s.db.QueryRowContext(ctx, `SELECT project_id FROM knowledge_sources WHERE id = ?`, id).Scan(&projectID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && projectID != "") {
		return ErrNotFound
	}
	return err
}
