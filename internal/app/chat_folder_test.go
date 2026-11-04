package app

import (
	"context"
	"testing"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/runlog"
	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestChatFolder(t *testing.T) {
	a := newRenameTestApp(t)
	dir := turnopts.Project(t.TempDir())
	ctx := context.Background()
	conv, err := a.Conversations.Create(ctx, "folder", "", "")
	if err != nil {
		t.Fatal(err)
	}
	as := func(role auth.Role, client string) context.Context {
		return auth.WithPrincipal(runlog.WithOrigin(ctx, runlog.Origin{Client: client}), auth.Principal{Person: auth.Person{ID: auth.OwnerID, Role: role}})
	}
	for name, c := range map[string]struct {
		ctx     context.Context
		project string
		want    string
	}{
		"none set":        {as(auth.RoleOwner, "web"), "", ""},
		"web":             {as(auth.RoleOwner, "web"), dir, dir},
		"desktop":         {as(auth.RoleAdmin, "desktop"), dir, dir},
		"terminal":        {as(auth.RoleAdmin, "terminal"), dir, dir},
		"a member":        {as(auth.RoleMember, "web"), dir, ""},
		"a phone":         {as(auth.RoleOwner, "phone"), dir, ""},
		"no client named": {as(auth.RoleOwner, ""), dir, ""},
		"folder gone":     {as(auth.RoleOwner, "web"), dir + "/gone", ""},
	} {
		got := turnopts.ProjectFrom(a.chatFolder(c.ctx, contracts.Conversation{ID: conv.ID, Project: c.project}))
		if got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
	// A terminal says where it is, and the chat remembers.
	named := turnopts.WithProject(as(auth.RoleOwner, "terminal"), dir)
	if got := turnopts.ProjectFrom(a.chatFolder(named, conv)); got != dir {
		t.Fatalf("named: %q", got)
	}
	saved, err := a.Conversations.Get(ctx, conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Project != dir {
		t.Fatalf("saved %q", saved.Project)
	}
}
