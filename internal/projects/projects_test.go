package projects

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/mimir"
	"github.com/yeixio/toskar-core/internal/muninn"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/store/repositories"
)

type fixture struct {
	svc     *Service
	chats   *repositories.ConversationRepo
	mimir   *mimir.Store
	memory  *muninn.Store
	deleted []string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	f := &fixture{
		chats:  repositories.NewConversationRepo(db.SQL),
		mimir:  mimir.NewStore(db.SQL, filepath.Join(t.TempDir(), "knowledge")),
		memory: muninn.NewStore(db.SQL),
	}
	f.svc = &Service{Store: NewStore(db.SQL), Files: f.mimir, Memories: f.memory,
		DeleteChats: func(ctx context.Context, ids []string) error {
			f.deleted = append(f.deleted, ids...)
			_, err := f.chats.DeleteMany(ctx, ids)
			return err
		}}
	return f
}

func as(id string) context.Context {
	return auth.WithPrincipal(context.Background(), auth.Principal{Person: auth.Person{ID: id, Role: auth.RoleMember}})
}

func ptr[T any](v T) *T { return &v }

func TestProjectsCRUD(t *testing.T) {
	f := newFixture(t)
	ctx := as("ana")
	if _, err := f.svc.Create(ctx, Fields{Name: ptr("  ")}); err == nil {
		t.Fatal("a project without a name was made")
	}
	p, err := f.svc.Create(ctx, Fields{Name: ptr(" Garden "), Instructions: ptr("Answer in metric."), ProfileID: ptr("chat")})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Garden" || p.Instructions != "Answer in metric." || p.ProfileID != "chat" || p.MemoryOff || len(p.Knowledge) != 0 {
		t.Fatalf("made %+v", p)
	}
	second, err := f.svc.Create(ctx, Fields{Name: ptr("Taxes"), MemoryOff: ptr(true)})
	if err != nil {
		t.Fatal(err)
	}
	// A chat in the first makes it the newest activity, with a count.
	chat, err := f.chats.Create(ctx, "plants", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.chats.SetProjectID(ctx, chat.ID, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.chats.Update(ctx, chat.ID, repositories.ConversationPatch{Title: ptr("plants and soil")}); err != nil {
		t.Fatal(err)
	}
	list, err := f.svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != p.ID || list[0].ChatCount != 1 || list[1].ID != second.ID || !list[1].MemoryOff {
		t.Fatalf("listed %+v", list)
	}
	got, err := f.svc.Update(ctx, p.ID, Fields{Instructions: ptr(""), MemoryOff: ptr(true)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Garden" || got.Instructions != "" || !got.MemoryOff || got.ProfileID != "chat" {
		t.Fatalf("updated %+v", got)
	}
	if _, err := f.svc.Update(ctx, p.ID, Fields{Knowledge: []string{"nope"}, SetKnowledge: true}); err == nil {
		t.Fatal("linked a source that doesn't exist")
	}
}

// Another person can't see, change, or delete someone's project, or add to
// its files.
func TestProjectsArePrivate(t *testing.T) {
	f := newFixture(t)
	ana, sam := as("ana"), as("sam")
	p, err := f.svc.Create(ana, Fields{Name: ptr("Garden")})
	if err != nil {
		t.Fatal(err)
	}
	if list, _ := f.svc.List(sam); len(list) != 0 {
		t.Fatalf("sam lists %+v", list)
	}
	if _, err := f.svc.Get(sam, p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("sam's get: %v", err)
	}
	if _, err := f.svc.Update(sam, p.ID, Fields{Name: ptr("Mine")}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("sam's update: %v", err)
	}
	if _, err := f.svc.AddFile(sam, p.ID, "notes.md", []byte("# Notes")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("sam's file: %v", err)
	}
	if err := f.svc.Delete(sam, p.ID, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("sam's delete: %v", err)
	}
	if got, err := f.svc.Get(ana, p.ID); err != nil || got.Name != "Garden" {
		t.Fatalf("ana's project: %+v %v", got, err)
	}
}

// Deleting a project keeps its chats unless asked, and removes its files'
// source and its memories.
func TestDeleteProject(t *testing.T) {
	for _, withChats := range []bool{false, true} {
		f := newFixture(t)
		ctx := as("ana")
		p, err := f.svc.Create(ctx, Fields{Name: ptr("Garden")})
		if err != nil {
			t.Fatal(err)
		}
		chat, _ := f.chats.Create(ctx, "plants", "", "")
		if err := f.chats.SetProjectID(ctx, chat.ID, p.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.svc.AddFile(ctx, p.ID, "beds.md", []byte("# Beds\n\nThe tomato bed gets morning sun.")); err != nil {
			t.Fatal(err)
		}
		source, _ := f.mimir.ProjectSourceID(ctx, p.ID)
		if source == "" {
			t.Fatal("no files source")
		}
		if _, _, err := f.memory.AddIn(ctx, p.ID, "The tomatoes are San Marzano", "", muninn.SourceExplicit, chat.ID); err != nil {
			t.Fatal(err)
		}
		mine, _, err := f.memory.Add(ctx, "I live in Juneau", "", muninn.SourceExplicit, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := f.svc.Delete(ctx, p.ID, withChats); err != nil {
			t.Fatal(err)
		}
		if _, err := f.svc.Get(ctx, p.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("still there: %v", err)
		}
		got, err := f.chats.Get(ctx, chat.ID)
		switch {
		case withChats && err == nil:
			t.Fatal("the chat was kept")
		case !withChats && (err != nil || got.ProjectID != ""):
			t.Fatalf("the chat: %+v %v", got, err)
		}
		if _, err := f.mimir.Get(mimir.WithProject(ctx, p.ID), source); !errors.Is(err, mimir.ErrNotFound) {
			t.Fatalf("the files' source: %v", err)
		}
		memories, _ := f.memory.List(ctx)
		if len(memories) != 1 || memories[0].ID != mine.ID {
			t.Fatalf("memories left: %+v", memories)
		}
	}
}

func TestProjectFiles(t *testing.T) {
	f := newFixture(t)
	ctx := as("ana")
	p, err := f.svc.Create(ctx, Fields{Name: ptr("Garden")})
	if err != nil {
		t.Fatal(err)
	}
	if ids := f.svc.KnowledgeIDs(ctx, p); len(ids) != 0 {
		t.Fatalf("no files yet, but %v", ids)
	}
	got, err := f.svc.AddFile(ctx, p.ID, "beds.md", []byte("# Beds\n\nThe tomato bed gets morning sun."))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Files) != 1 || got.Files[0].Name != "beds.md" {
		t.Fatalf("files %+v", got.Files)
	}
	if _, err := f.svc.AddFile(ctx, p.ID, "seeds.csv", []byte("seed,count\nbasil,40\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AddFile(ctx, p.ID, "tool.exe", []byte("MZ")); err == nil {
		t.Fatal("added a file Toskar can't read")
	}
	ids := f.svc.KnowledgeIDs(ctx, got)
	if len(ids) != 1 {
		t.Fatalf("knowledge ids %v", ids)
	}
	hits, err := f.mimir.Search(mimir.WithProject(ctx, p.ID), mimir.SearchInput{Query: "basil seeds", SourceIDs: ids})
	if err != nil || len(hits) == 0 {
		t.Fatalf("search: %+v %v", hits, err)
	}
	// The project's files aren't on the Knowledge page.
	if all, _ := f.mimir.List(ctx); len(all) != 0 {
		t.Fatalf("listed %+v", all)
	}
	// Nor can they be linked to another project.
	if _, err := f.svc.Create(ctx, Fields{Name: ptr("Other"), Knowledge: ids, SetKnowledge: true}); err == nil {
		t.Fatal("linked another project's files")
	}
	if err := f.svc.RemoveFile(ctx, p.ID, "beds.md"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RemoveFile(ctx, p.ID, "beds.md"); !errors.Is(err, mimir.ErrProjectFileNotFound) {
		t.Fatalf("removed twice: %v", err)
	}
	if err := f.svc.RemoveFile(ctx, p.ID, "seeds.csv"); err != nil {
		t.Fatal(err)
	}
	if id, _ := f.mimir.ProjectSourceID(ctx, p.ID); id != "" {
		t.Fatal("the source outlived its last file")
	}
}
