package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/store"
)

// An Admin from the environment at first start (#592), once, with the
// password from a file; a second start with other values adds no one.
func TestSeedAdmin(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	a := &App{DB: db, People: auth.NewPeople(db.SQL), Logger: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	ctx := context.Background()
	secret := filepath.Join(dir, "password")
	if err := os.WriteFile(secret, []byte("a strong password\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("TOSKAR_ADMIN_EMAIL", "ops+toskar@example.com")
	if err := a.seedAdmin(ctx); err == nil {
		t.Fatal("an email without a password file was accepted")
	}
	t.Setenv("TOSKAR_ADMIN_PASSWORD_FILE", secret)
	if err := a.seedAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := a.People.SignIn(ctx, "ops.toskar", "a strong password")
	if err != nil || p.Role != auth.RoleAdmin || p.Email != "ops+toskar@example.com" || p.MustChange {
		t.Fatalf("signed in as %+v: %v", p, err)
	}

	t.Setenv("TOSKAR_ADMIN_EMAIL", "someone@example.com")
	if err := a.seedAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	list, _ := a.People.List(ctx)
	if len(list) != 2 {
		t.Fatalf("people after a second start: %+v", list)
	}

	for in, want := range map[string]string{"ops+toskar@example.com": "ops.toskar", "a@example.com": "admin", "ann.lee@x.org": "ann.lee"} {
		if got := usernameFromEmail(in); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
}
