package mimir

import (
	"context"
	"errors"
	"testing"
)

// A project's files are reached only from that project (#1136): by id
// anywhere else, through Get, Search, Update, Content, Refresh, or Delete,
// they read as not found.
func TestProjectFilesStayInTheirProject(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.AddProjectFile(ctx, "garden", "beds.md", []byte("# Beds\n\nThe tomato bed gets morning sun.")); err != nil {
		t.Fatal(err)
	}
	id, err := s.ProjectSourceID(ctx, "garden")
	if err != nil || id == "" {
		t.Fatalf("source %q %v", id, err)
	}
	other, err := s.Create(ctx, CreateInput{Kind: KindText, Filename: "shop.md", Text: "The shop opens at nine."})
	if err != nil {
		t.Fatal(err)
	}
	search := func(ctx context.Context) int {
		hits, err := s.Search(ctx, SearchInput{Query: "tomato bed morning sun", SourceIDs: []string{id, other.ID}})
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, h := range hits {
			if h.SourceID == id {
				n++
			}
		}
		return n
	}
	for name, ctx := range map[string]context.Context{"outside a project": ctx, "in another project": WithProject(ctx, "taxes")} {
		if _, err := s.Get(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s, get: %v", name, err)
		}
		if n := search(ctx); n != 0 {
			t.Errorf("%s, search found %d passages", name, n)
		}
		renamed := "mine"
		if _, err := s.Update(ctx, id, UpdateInput{Name: &renamed}); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s, update: %v", name, err)
		}
		if _, err := s.Content(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s, content: %v", name, err)
		}
		if err := s.Refresh(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s, refresh: %v", name, err)
		}
		if err := s.Delete(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s, delete: %v", name, err)
		}
	}
	// In the project, it's there; the Knowledge page's sources are too.
	in := WithProject(ctx, "garden")
	if _, err := s.Get(in, id); err != nil {
		t.Fatalf("in the project: %v", err)
	}
	if n := search(in); n == 0 {
		t.Fatal("the project's own chat didn't find its file")
	}
	if _, err := s.Get(in, other.ID); err != nil {
		t.Fatalf("a Knowledge page source in a project: %v", err)
	}
}
