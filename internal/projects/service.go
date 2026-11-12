package projects

import (
	"context"
	"errors"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Files keeps a project's files as knowledge: Mimir.
type Files interface {
	ProjectSourceID(ctx context.Context, projectID string) (string, error)
	AddProjectFile(ctx context.Context, projectID, name string, content []byte) error
	RemoveProjectFile(ctx context.Context, projectID, name string) error
	ProjectFiles(ctx context.Context, projectID string) ([]contracts.ProjectFile, error)
	DeleteProjectFiles(ctx context.Context, projectID string) error
	// Linkable says whether a source on the Knowledge page can be linked.
	Linkable(ctx context.Context, sourceID string) error
}

// Memories keeps what a project's chats learn: Muninn.
type Memories interface {
	DeleteProject(ctx context.Context, projectID string) error
}

// Service is projects with their files, memories, and chats.
type Service struct {
	Store    *Store
	Files    Files
	Memories Memories
	// DeleteChats deletes chats with their files, when a project is deleted
	// with its chats.
	DeleteChats func(ctx context.Context, ids []string) error
}

// ErrNoFiles is a file added where files can't be kept.
var ErrNoFiles = contracts.NewError("NOT_IMPLEMENTED", nil, errors.New("project files are not available"))

// List returns the person's projects, newest activity first, with their files.
func (s *Service) List(ctx context.Context) ([]contracts.Project, error) {
	list, err := s.Store.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].Files, err = s.files(ctx, list[i].ID); err != nil {
			return nil, err
		}
	}
	return list, nil
}

// Get returns one of the person's projects with its files.
func (s *Service) Get(ctx context.Context, id string) (contracts.Project, error) {
	p, err := s.Store.Get(ctx, id)
	if err != nil {
		return contracts.Project{}, err
	}
	p.Files, err = s.files(ctx, id)
	return p, err
}

func (s *Service) files(ctx context.Context, id string) ([]contracts.ProjectFile, error) {
	if s.Files == nil {
		return []contracts.ProjectFile{}, nil
	}
	return s.Files.ProjectFiles(ctx, id)
}

// checkKnowledge refuses a linked source that isn't on the Knowledge page.
func (s *Service) checkKnowledge(ctx context.Context, f Fields) error {
	if !f.SetKnowledge {
		return nil
	}
	for _, id := range f.Knowledge {
		if s.Files == nil {
			return ErrNoFiles
		}
		if err := s.Files.Linkable(ctx, id); err != nil {
			return contracts.Errorf("KNOWLEDGE_SOURCE_NOT_FOUND", map[string]any{"source_id": id}, "there's no knowledge source %q to link", id)
		}
	}
	return nil
}

// Create makes a project for the person.
func (s *Service) Create(ctx context.Context, f Fields) (contracts.Project, error) {
	if err := s.checkKnowledge(ctx, f); err != nil {
		return contracts.Project{}, err
	}
	return s.Store.Create(ctx, f)
}

// Update changes one of the person's projects.
func (s *Service) Update(ctx context.Context, id string, f Fields) (contracts.Project, error) {
	if err := s.checkKnowledge(ctx, f); err != nil {
		return contracts.Project{}, err
	}
	if _, err := s.Store.Update(ctx, id, f); err != nil {
		return contracts.Project{}, err
	}
	return s.Get(ctx, id)
}

// Delete removes one of the person's projects with its files and memories.
// Its chats move out of it, or with deleteChats are deleted too.
func (s *Service) Delete(ctx context.Context, id string, deleteChats bool) error {
	if _, err := s.Store.Get(ctx, id); err != nil {
		return err
	}
	if deleteChats && s.DeleteChats != nil {
		ids, err := s.Store.ChatIDs(ctx, id)
		if err != nil {
			return err
		}
		if len(ids) > 0 {
			if err := s.DeleteChats(ctx, ids); err != nil {
				return err
			}
		}
	}
	if s.Files != nil {
		if err := s.Files.DeleteProjectFiles(ctx, id); err != nil {
			return err
		}
	}
	if s.Memories != nil {
		if err := s.Memories.DeleteProject(ctx, id); err != nil {
			return err
		}
	}
	return s.Store.Delete(ctx, id)
}

// AddFile adds a file to one of the person's projects, or replaces the one
// with its name.
func (s *Service) AddFile(ctx context.Context, id, name string, content []byte) (contracts.Project, error) {
	if _, err := s.Store.Get(ctx, id); err != nil {
		return contracts.Project{}, err
	}
	if s.Files == nil {
		return contracts.Project{}, ErrNoFiles
	}
	if err := s.Files.AddProjectFile(ctx, id, name, content); err != nil {
		return contracts.Project{}, err
	}
	_ = s.Store.Touch(ctx, id)
	return s.Get(ctx, id)
}

// RemoveFile takes a file out of one of the person's projects.
func (s *Service) RemoveFile(ctx context.Context, id, name string) error {
	if _, err := s.Store.Get(ctx, id); err != nil {
		return err
	}
	if s.Files == nil {
		return ErrNoFiles
	}
	if err := s.Files.RemoveProjectFile(ctx, id, name); err != nil {
		return err
	}
	return s.Store.Touch(ctx, id)
}

// KnowledgeIDs are the sources a chat in the project searches: its files'
// source, then the linked ones.
func (s *Service) KnowledgeIDs(ctx context.Context, p contracts.Project) []string {
	var ids []string
	if s.Files != nil {
		if id, err := s.Files.ProjectSourceID(ctx, p.ID); err == nil && id != "" {
			ids = append(ids, id)
		}
	}
	return append(ids, p.Knowledge...)
}
