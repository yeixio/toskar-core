package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/config"
)

// seedAdmin adds an Admin from the environment at first start (#592), so a
// fresh Docker or Kubernetes install can be set up without a browser:
// TOSKAR_ADMIN_EMAIL and TOSKAR_ADMIN_PASSWORD_FILE (a file, such as a
// mounted secret, so the password isn't in the environment), with
// TOSKAR_ADMIN_USERNAME and TOSKAR_ADMIN_NAME optional. Once there is an
// Admin, it does nothing, so changing them later doesn't make another.
func (a *App) seedAdmin(ctx context.Context) error {
	email := strings.TrimSpace(config.Env("ADMIN_EMAIL"))
	file := strings.TrimSpace(config.Env("ADMIN_PASSWORD_FILE"))
	if email == "" && file == "" {
		return nil
	}
	if email == "" || file == "" {
		return errors.New("TOSKAR_ADMIN_EMAIL and TOSKAR_ADMIN_PASSWORD_FILE go together")
	}
	if a.People == nil {
		return nil
	}
	list, err := a.People.List(ctx)
	if err != nil {
		return err
	}
	for _, p := range list {
		if p.Role == auth.RoleAdmin {
			return nil
		}
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("TOSKAR_ADMIN_PASSWORD_FILE: %w", err)
	}
	password := strings.TrimRight(string(raw), "\r\n")
	username := strings.TrimSpace(config.Env("ADMIN_USERNAME"))
	if username == "" {
		username = usernameFromEmail(email)
	}
	name := strings.TrimSpace(config.Env("ADMIN_NAME"))
	if name == "" {
		name = "Admin"
	}
	ctx = auth.WithSystem(ctx)
	person, err := a.People.CreateWith(ctx, name, auth.RoleAdmin, email)
	if err != nil {
		return fmt.Errorf("add the Admin from TOSKAR_ADMIN_EMAIL: %w", err)
	}
	if err := a.People.SetPasswordFor(ctx, person.ID, username, password, false); err != nil {
		_ = a.People.Delete(ctx, person.ID)
		return fmt.Errorf("add the Admin from TOSKAR_ADMIN_EMAIL: %w", err)
	}
	auth.NewActivity(a.DB.SQL).Record(ctx, person, "added", "role admin, from the environment")
	a.Logger.Info("added an Admin from the environment", "username", username, "email", email)
	return nil
}

var usernameUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// usernameFromEmail is the part of an address before @, as a username:
// other characters become dots, and too short is "admin".
func usernameFromEmail(email string) string {
	local, _, _ := strings.Cut(email, "@")
	name := strings.Trim(usernameUnsafe.ReplaceAllString(local, "."), ".")
	if len(name) > 32 {
		name = name[:32]
	}
	if len(name) < 3 {
		return "admin"
	}
	return name
}
