package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/muninn"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/store/repositories"
)

// Removing a person (#592) deletes their chats and memories, or gives
// them to someone else, revokes their keys, and leaves others' data alone.
func TestRemovePerson(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	a := &App{DB: db, People: auth.NewPeople(db.SQL), APIKeys: auth.NewAPIKeyManager(db.SQL, auth.NewSecretStore(dir)),
		Conversations: repositories.NewConversationRepo(db.SQL), Muninn: muninn.NewStore(db.SQL),
		Automations: repositories.NewAutomationRepo(db.SQL), Artifacts: artifacts.NewStore(db.SQL, filepath.Join(dir, "artifacts"))}
	ctx := context.Background()

	seed := func(name string) (auth.Person, context.Context) {
		p, err := a.People.Create(ctx, name, auth.RoleMember)
		if err != nil {
			t.Fatal(err)
		}
		theirs := auth.AsPerson(ctx, p)
		if _, err := a.Conversations.Create(theirs, name+"'s chat", "", ""); err != nil {
			t.Fatal(err)
		}
		if _, _, err := a.Muninn.Add(theirs, name+" likes tea", "preferences", "explicit", ""); err != nil {
			t.Fatal(err)
		}
		if _, _, err := a.APIKeys.Create(theirs, name+"'s key"); err != nil {
			t.Fatal(err)
		}
		return p, theirs
	}
	count := func(ctx context.Context) (chats, memories int) {
		list, _ := a.Conversations.List(ctx)
		mems, _ := a.Muninn.List(ctx)
		return len(list), len(mems)
	}

	dee, _ := seed("Dee")
	eli, eliCtx := seed("Eli")
	fay, fayCtx := seed("Fay")

	// Dee's data is deleted; Eli's and Fay's stay.
	if err := a.RemovePerson(ctx, dee.ID, ""); err != nil {
		t.Fatal(err)
	}
	var left int
	_ = db.SQL.QueryRow(`SELECT COUNT(*) FROM conversations WHERE person_id = ?`, dee.ID).Scan(&left)
	if left != 0 {
		t.Fatalf("Dee's chats left: %d", left)
	}
	if c, m := count(eliCtx); c != 1 || m != 1 {
		t.Fatalf("Eli has %d chats, %d memories", c, m)
	}

	// Eli's data goes to Fay.
	if err := a.RemovePerson(ctx, eli.ID, fay.ID); err != nil {
		t.Fatal(err)
	}
	if c, m := count(fayCtx); c != 2 || m != 2 {
		t.Fatalf("Fay has %d chats, %d memories after the transfer", c, m)
	}
	keys, _ := a.APIKeys.List(ctx)
	for _, k := range keys {
		if k.PersonID == dee.ID || k.PersonID == eli.ID {
			t.Fatalf("a removed person's key is still live: %+v", k)
		}
	}
	if _, err := a.People.Get(ctx, eli.ID); err == nil {
		t.Fatal("Eli is still there")
	}

	// Not to themselves, not the Owner.
	if err := a.RemovePerson(ctx, fay.ID, fay.ID); err != ErrTransferToSelf {
		t.Fatalf("to self: %v", err)
	}
	if err := a.RemovePerson(ctx, auth.OwnerID, ""); err != auth.ErrOwnerFixed {
		t.Fatalf("the Owner: %v", err)
	}
}
