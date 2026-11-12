package muninn

import (
	"context"
	"testing"

	"github.com/yeixio/toskar-core/internal/auth"
)

func as(id string) context.Context {
	return auth.WithPrincipal(context.Background(), auth.Principal{Person: auth.Person{ID: id, Role: auth.RoleMember}, Via: auth.ViaAPIKey})
}

// Memories are their person's (#206): another person's never reach a turn,
// a list, or "forget that".
func TestMemoriesArePrivate(t *testing.T) {
	s := newStore(t)
	owner, sam := as(auth.OwnerID), as("sam")
	theirs, _, err := s.Add(sam, "My name is Sam and I prefer tea", "", "chat", "")
	if err != nil {
		t.Fatal(err)
	}
	if list, _ := s.List(owner); len(list) != 0 {
		t.Fatalf("the owner lists %+v", list)
	}
	if got, _ := s.Relevant(owner, "what tea do I prefer, Sam?", ""); len(got) != 0 {
		t.Fatalf("sam's memory reached the owner's turn: %+v", got)
	}
	if got, _ := s.Relevant(sam, "what tea do I prefer?", ""); len(got) == 0 {
		t.Fatal("sam's own memory didn't reach their turn")
	}
	if _, ok, _ := s.Find(owner, "Sam prefer tea", ""); ok {
		t.Fatal("the owner found sam's memory to forget")
	}
	if m, ok, err := s.Find(sam, "prefer tea", ""); !ok || err != nil || m.ID != theirs.ID {
		t.Fatalf("sam's own find = %+v %v %v", m, ok, err)
	}
	if err := s.Delete(owner, theirs.ID); err == nil {
		t.Fatal("the owner deleted sam's memory")
	}
	if _, err := s.Get(owner, theirs.ID); err == nil {
		t.Fatal("the owner read sam's memory")
	}
	// The same words for the owner are the owner's own memory, not a duplicate of sam's.
	mine, added, _ := s.Add(owner, "My name is Sam and I prefer tea", "", "chat", "")
	if !added || mine.ID == theirs.ID {
		t.Fatalf("owner's add = %+v added=%v", mine, added)
	}
}
