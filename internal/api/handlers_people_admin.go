package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/auth"
)

// Managing people without a browser (#592): one person, filters, a
// password an Admin sets, removing someone, and their keys and devices.

func (s *Server) peopleAdminRoutes(api *mux.Router) {
	// Before /people/{id}, which would take "activity" as an id.
	api.HandleFunc("/people/activity", s.atLeast(auth.RoleAdmin, s.handlePeopleActivity)).Methods(http.MethodGet)
	api.HandleFunc("/people/{id}", s.atLeast(auth.RoleAdmin, s.handleGetPerson)).Methods(http.MethodGet)
	api.HandleFunc("/people/{id}", s.atLeast(auth.RoleAdmin, s.handleRemovePerson)).Methods(http.MethodDelete)
	api.HandleFunc("/people/{id}/keys", s.atLeast(auth.RoleAdmin, s.handlePersonKeys(false))).Methods(http.MethodGet)
	api.HandleFunc("/people/{id}/keys/{key}", s.atLeast(auth.RoleAdmin, s.handleRevokePersonKey(false))).Methods(http.MethodDelete)
	api.HandleFunc("/people/{id}/devices", s.atLeast(auth.RoleAdmin, s.handlePersonKeys(true))).Methods(http.MethodGet)
	api.HandleFunc("/people/{id}/devices/{key}", s.atLeast(auth.RoleAdmin, s.handleRevokePersonKey(true))).Methods(http.MethodDelete)
	api.HandleFunc("/me/password", s.handleChangeMyPassword).Methods(http.MethodPost)
}

// mustChangeRoutes are what someone whose password an Admin set reaches
// before changing it: who they are, changing it, and signing out.
var mustChangeRoutes = map[string]bool{
	"GET /api/v1/me":           true,
	"POST /api/v1/me/password": true,
	"DELETE /api/v1/session":   true,
	"GET /api/v1/health":       true,
	"GET /api/v1/version":      true,
	"GET /api/v1/settings":     true,
}

// record logs a change to target in the people activity log.
func (s *Server) record(ctx context.Context, target auth.Person, action, detail string) {
	s.deps.PeopleActivity.Record(ctx, target, action, detail)
}

// recordChange logs what PATCH /people/{id} changed.
func (s *Server) recordChange(ctx context.Context, before, after auth.Person) {
	var parts []string
	if before.Name != after.Name {
		parts = append(parts, "name "+before.Name+" → "+after.Name)
	}
	if before.Role != after.Role {
		parts = append(parts, "role "+string(before.Role)+" → "+string(after.Role))
	}
	if before.Email != after.Email {
		parts = append(parts, "email")
	}
	if len(parts) > 0 {
		s.record(ctx, after, "changed", strings.Join(parts, ", "))
	}
	switch {
	case before.Disabled == nil && after.Disabled != nil:
		s.record(ctx, after, "disabled", "")
	case before.Disabled != nil && after.Disabled == nil:
		s.record(ctx, after, "enabled", "")
	}
}

// managed is the person in the route, when the request's person may
// manage them: themselves, or someone mayManage allows.
func (s *Server) managed(w http.ResponseWriter, r *http.Request) (auth.Person, bool) {
	if !s.signInReady(w) {
		return auth.Person{}, false
	}
	target, err := s.deps.People.Get(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		peopleError(w, err)
		return auth.Person{}, false
	}
	actor := auth.PrincipalFrom(r.Context()).Person
	if target.ID != actor.ID && !mayManage(actor, target.Role) {
		writeErr(w, http.StatusForbidden, "ROLE_REQUIRED", "only the Owner manages Admins", map[string]any{"role": string(auth.RoleOwner)})
		return auth.Person{}, false
	}
	return target, true
}

func (s *Server) handleGetPerson(w http.ResponseWriter, r *http.Request) {
	if !s.signInReady(w) {
		return
	}
	person, err := s.deps.People.Get(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		peopleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, person)
}

// filterPeople keeps those with ?role= and ?disabled=true or false.
func filterPeople(list []auth.Person, r *http.Request) []auth.Person {
	role := auth.Role(r.URL.Query().Get("role"))
	disabled, byDisabled := r.URL.Query().Get("disabled"), false
	want := false
	if disabled != "" {
		if b, err := strconv.ParseBool(disabled); err == nil {
			byDisabled, want = true, b
		}
	}
	out := []auth.Person{}
	for _, p := range list {
		if role != "" && p.Role != role {
			continue
		}
		if byDisabled && (p.Disabled != nil) != want {
			continue
		}
		out = append(out, p)
	}
	return out
}

// newPerson is POST /people's body: a name and role, and optionally an
// email address and a password to sign in with at once (#592).
type newPerson struct {
	Name       string    `json:"name"`
	Role       auth.Role `json:"role"`
	Email      string    `json:"email"`
	Username   string    `json:"username"`
	Password   string    `json:"password"`
	MustChange bool      `json:"must_change"`
}

// passwordChange is the part of PATCH /people/{id} that sets a password:
// with a username when they have none yet, and must_change to have them
// change it at their next sign-in.
type passwordChange struct {
	Password   *string `json:"password,omitempty"`
	Username   string  `json:"username,omitempty"`
	MustChange bool    `json:"must_change,omitempty"`
}

// setPassword sets target's password from an Admin, signs them out
// everywhere, and logs it, never with the password.
func (s *Server) setPassword(ctx context.Context, target auth.Person, c passwordChange) error {
	username := strings.TrimSpace(c.Username)
	if username == "" {
		username = target.Username
	}
	if err := s.deps.People.SetPasswordFor(ctx, target.ID, username, *c.Password, c.MustChange); err != nil {
		return err
	}
	_ = s.deps.Sessions.DeleteFor(ctx, target.ID)
	detail := ""
	if c.MustChange {
		detail = "must change it at next sign-in"
	}
	s.record(ctx, target, "password_set", detail)
	return nil
}

// handleRemovePerson removes someone (#592). The body's confirm must be
// their name; ?transfer_to= gives their data to another person instead of
// deleting it.
func (s *Server) handleRemovePerson(w http.ResponseWriter, r *http.Request) {
	if s.deps.RemovePerson == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Removing people isn't available.", nil)
		return
	}
	target, ok := s.managed(w, r)
	if !ok {
		return
	}
	actor := auth.PrincipalFrom(r.Context()).Person
	if target.ID == actor.ID || target.Role == auth.RoleOwner {
		writeErr(w, http.StatusForbidden, "OWNER_FIXED", "you can't remove yourself or the Owner", nil)
		return
	}
	var body struct {
		Confirm string `json:"confirm"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body)
	if strings.TrimSpace(body.Confirm) != target.Name {
		writeErr(w, http.StatusBadRequest, "CONFIRM_NAME", "to remove "+target.Name+", send confirm with their name", map[string]any{"name": target.Name})
		return
	}
	transferTo := r.URL.Query().Get("transfer_to")
	var to auth.Person
	if transferTo != "" {
		var err error
		if to, err = s.deps.People.Active(r.Context(), transferTo); err != nil {
			writeErr(w, http.StatusBadRequest, "TRANSFER_INVALID", "there's no active person to give their data to with that id", nil)
			return
		}
	}
	if err := s.deps.RemovePerson(r.Context(), target.ID, to.ID); err != nil {
		peopleError(w, err)
		return
	}
	detail := "their data deleted"
	if to.ID != "" {
		detail = "their data given to " + to.Name
	}
	s.record(r.Context(), target, "deleted", detail)
	w.WriteHeader(http.StatusNoContent)
}

// personKeys are target's keys: their devices, or their other keys.
func (s *Server) personKeys(ctx context.Context, target auth.Person, devices bool) ([]auth.APIKeyRecord, error) {
	all, err := s.deps.ListAPIKeys(ctx)
	if err != nil {
		return nil, err
	}
	out := []auth.APIKeyRecord{}
	for _, k := range all {
		if k.PersonID == target.ID && (k.Kind == auth.KindDevice) == devices {
			out = append(out, k)
		}
	}
	return out, nil
}

func (s *Server) handlePersonKeys(devices bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.deps.ListAPIKeys == nil {
			writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Keys aren't available.", nil)
			return
		}
		target, ok := s.managed(w, r)
		if !ok {
			return
		}
		list, err := s.personKeys(r.Context(), target, devices)
		if err != nil {
			writeErrFrom(w, http.StatusInternalServerError, "KEYS_FAILED", err)
			return
		}
		writeJSON(w, http.StatusOK, list)
	}
}

func (s *Server) handleRevokePersonKey(devices bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.deps.ListAPIKeys == nil || s.deps.RevokeAPIKey == nil {
			writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Keys aren't available.", nil)
			return
		}
		target, ok := s.managed(w, r)
		if !ok {
			return
		}
		list, err := s.personKeys(r.Context(), target, devices)
		if err != nil {
			writeErrFrom(w, http.StatusInternalServerError, "KEYS_FAILED", err)
			return
		}
		id := mux.Vars(r)["key"]
		for _, k := range list {
			if k.ID != id {
				continue
			}
			if err := s.deps.RevokeAPIKey(r.Context(), id); err != nil {
				writeErrFrom(w, http.StatusInternalServerError, "KEYS_FAILED", err)
				return
			}
			action := "key_revoked"
			if devices {
				action = "device_revoked"
			}
			s.record(r.Context(), target, action, k.Name)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeErr(w, http.StatusNotFound, "KEY_NOT_FOUND", "they have no key with that id", nil)
	}
}

func (s *Server) handlePeopleActivity(w http.ResponseWriter, r *http.Request) {
	if s.deps.PeopleActivity == nil {
		writeJSON(w, http.StatusOK, []auth.ActivityEntry{})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := s.deps.PeopleActivity.List(r.Context(), r.URL.Query().Get("person"), limit)
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "INTERNAL_ERROR", err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleChangeMyPassword changes the request's person's own password, with
// their current one, as when an Admin set it and it must be changed.
func (s *Server) handleChangeMyPassword(w http.ResponseWriter, r *http.Request) {
	if !s.signInReady(w) {
		return
	}
	var body struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return
	}
	ctx := r.Context()
	me := auth.PrincipalFrom(ctx).Person
	if !s.deps.People.HasSignIn(ctx, me.ID) {
		writeErr(w, http.StatusBadRequest, "NO_PASSWORD", "you don't sign in with a password here", nil)
		return
	}
	// Tries count as sign-ins do, so a session can't guess the password.
	name := strings.ToLower(me.Username)
	if signInByName.Blocked(name) {
		writeErr(w, http.StatusTooManyRequests, "SIGN_IN_THROTTLED", "too many tries; wait 15 minutes and try again", nil)
		return
	}
	if _, err := s.deps.People.SignIn(ctx, me.Username, body.Current); err != nil {
		signInByName.Fail(name)
		writeErr(w, http.StatusForbidden, "PASSWORD_WRONG", "that isn't your current password", nil)
		return
	}
	signInByName.Forget(name)
	if body.New == body.Current {
		writeErr(w, http.StatusBadRequest, "PASSWORD_SAME", "choose a password other than your current one", nil)
		return
	}
	if err := s.deps.People.SetPassword(ctx, me.ID, body.New); err != nil {
		if errors.Is(err, auth.ErrWeakPassword) {
			peopleError(w, err)
			return
		}
		writeErrFrom(w, http.StatusInternalServerError, "INTERNAL_ERROR", err)
		return
	}
	// Signed out everywhere else; this browser stays signed in.
	_ = s.deps.Sessions.DeleteFor(ctx, me.ID)
	person, _ := s.deps.People.Active(ctx, me.ID)
	if auth.PrincipalFrom(ctx).Via == auth.ViaSession {
		if err := s.setSession(w, r, person); err != nil {
			writeErrFrom(w, http.StatusInternalServerError, "INTERNAL_ERROR", err)
			return
		}
	}
	s.record(ctx, person, "password_changed", "")
	writeJSON(w, http.StatusOK, auth.Principal{Person: person, Via: auth.PrincipalFrom(ctx).Via})
}
