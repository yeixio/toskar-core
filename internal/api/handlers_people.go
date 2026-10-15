package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Signing in and people (#206). A person signs in with a username and
// password, which they choose from a one-time invite link; a browser then
// carries a session cookie. Admins and the Owner add people and send them
// links.

// Failed sign-ins: five per username in 15 minutes, twenty per address in
// ten, then that username or address waits.
var (
	signInByName    = auth.NewLimiter(5, 15*time.Minute)
	signInByAddress = auth.NewLimiter(20, 10*time.Minute)
)

func (s *Server) peopleRoutes(api *mux.Router) {
	api.HandleFunc("/me", s.handleMe).Methods(http.MethodGet)
	api.HandleFunc("/me/preferences", s.handleMyPreferences).Methods(http.MethodPatch)
	api.HandleFunc("/session", s.handleSignIn).Methods(http.MethodPost)
	api.HandleFunc("/session", s.handleSignOut).Methods(http.MethodDelete)
	api.HandleFunc("/invites/{token}", s.handlePeekInvite).Methods(http.MethodGet)
	api.HandleFunc("/invites/{token}", s.handleAcceptInvite).Methods(http.MethodPost)
	api.HandleFunc("/people", s.atLeast(auth.RoleAdmin, s.handleListPeople)).Methods(http.MethodGet)
	api.HandleFunc("/people", s.atLeast(auth.RoleAdmin, s.handleAddPerson)).Methods(http.MethodPost)
	api.HandleFunc("/people/{id}", s.atLeast(auth.RoleAdmin, s.handleChangePerson)).Methods(http.MethodPatch)
	api.HandleFunc("/people/{id}/link", s.atLeast(auth.RoleAdmin, s.handlePersonLink)).Methods(http.MethodPost)
	s.pinRoutes(api)
	s.peopleAdminRoutes(api)
	s.personCapabilityRoutes(api)
}

// atLeast refuses a request whose person's role is below min.
func (s *Server) atLeast(min auth.Role, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !auth.PrincipalFrom(r.Context()).Person.Role.AtLeast(min) {
			writeErr(w, http.StatusForbidden, "ROLE_REQUIRED", "this needs the "+string(min)+" role", map[string]any{"role": string(min)})
			return
		}
		h(w, r)
	}
}

// myPreferences are the settings each person keeps for themselves (#206).
var myPreferences = map[string]bool{"ui_locale": true, "assistant_language_mode": true, "assistant_language": true}

// handleMyPreferences saves the request's person's own App language and
// assistant language, whatever their role, and answers with their
// settings.
func (s *Server) handleMyPreferences(w http.ResponseWriter, r *http.Request) {
	if s.deps.UpdateSettings == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Settings are not available.", nil)
		return
	}
	var in map[string]any
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be JSON.", nil)
		return
	}
	patch := map[string]any{}
	for k, v := range in {
		if !myPreferences[k] {
			writeErr(w, http.StatusBadRequest, "INVALID_SETTING", "Only ui_locale, assistant_language_mode, and assistant_language are your own.", map[string]any{"setting": k})
			return
		}
		if _, ok := v.(string); !ok {
			writeErr(w, http.StatusBadRequest, "INVALID_SETTING", "Each preference is a string.", map[string]any{"setting": k})
			return
		}
		patch[k] = v
	}
	view, err := s.deps.UpdateSettings(r.Context(), patch)
	if err != nil {
		if code, _ := contracts.ErrorCode(err); code != "" {
			writeErrFrom(w, http.StatusBadRequest, code, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, "SETTINGS_UPDATE_FAILED", "Could not update settings.", map[string]any{"cause": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) signInReady(w http.ResponseWriter) bool {
	if s.deps.People == nil || s.deps.Sessions == nil || s.deps.Invites == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Signing in isn't available.", nil)
		return false
	}
	return true
}

// handleMe is who the request is from (#206): their person, role, and how
// Toskar knows, so a client can show what that person may do.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, struct {
		auth.Principal
		// Profiles are the profiles they may chat with, when they're
		// pinned to some (#345).
		Profiles []string `json:"profiles,omitempty"`
		// Capabilities are what they may use (#589), so an app hides the
		// rest.
		Capabilities auth.Capabilities `json:"capabilities"`
	}{auth.PrincipalFrom(r.Context()), s.myProfiles(r), s.capabilitiesOf(r.Context(), auth.PrincipalFrom(r.Context()).Person)})
}

// setSession signs the browser in as person.
func (s *Server) setSession(w http.ResponseWriter, r *http.Request, person auth.Person) error {
	token, expires, err := s.deps.Sessions.Create(r.Context(), person.ID, r.UserAgent(), remoteIP(r))
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: auth.SessionCookie, Value: token, Path: "/", Expires: expires,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil,
	})
	return nil
}

func (s *Server) handleSignIn(w http.ResponseWriter, r *http.Request) {
	if !s.signInReady(w) {
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return
	}
	name, address := strings.ToLower(strings.TrimSpace(body.Username)), remoteIP(r)
	if signInByName.Blocked(name) || signInByAddress.Blocked(address) {
		writeErr(w, http.StatusTooManyRequests, "SIGN_IN_THROTTLED", "too many tries; wait 15 minutes and try again", nil)
		return
	}
	person, err := s.deps.People.SignIn(r.Context(), body.Username, body.Password)
	if err != nil {
		signInByName.Fail(name)
		signInByAddress.Fail(address)
		writeErr(w, http.StatusUnauthorized, "SIGN_IN_FAILED", auth.ErrSignIn.Error(), nil)
		return
	}
	signInByName.Forget(name)
	if err := s.setSession(w, r, person); err != nil {
		writeErr(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, auth.Principal{Person: person, Via: auth.ViaSession})
}

func (s *Server) handleSignOut(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(auth.SessionCookie); err == nil && s.deps.Sessions != nil {
		_ = s.deps.Sessions.Delete(r.Context(), cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: auth.SessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil})
	w.WriteHeader(http.StatusNoContent)
}

// inviteView is what an invite link's page shows before it's used.
type inviteView struct {
	Name string `json:"name"`
	// Kind is invite, to choose a username and password, or reset, for a
	// new password.
	Kind     string `json:"kind"`
	Username string `json:"username,omitempty"`
}

func (s *Server) handlePeekInvite(w http.ResponseWriter, r *http.Request) {
	if !s.signInReady(w) {
		return
	}
	id, kind, err := s.deps.Invites.Peek(r.Context(), mux.Vars(r)["token"])
	if err != nil {
		writeErr(w, http.StatusGone, "INVITE_INVALID", err.Error(), nil)
		return
	}
	person, err := s.deps.People.Active(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusGone, "INVITE_INVALID", auth.ErrNoInvite.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, inviteView{Name: person.Name, Kind: kind, Username: person.Username})
}

// peopleError writes a refusal from changing people or sign-ins, with a
// code the app has words for.
func peopleError(w http.ResponseWriter, err error) {
	msg := err.Error()
	switch {
	case errors.Is(err, auth.ErrUsernameTaken):
		writeErr(w, http.StatusConflict, "USERNAME_TAKEN", msg, nil)
	case errors.Is(err, auth.ErrBadUsername):
		writeErr(w, http.StatusBadRequest, "USERNAME_INVALID", msg, nil)
	case errors.Is(err, auth.ErrWeakPassword):
		writeErr(w, http.StatusBadRequest, "PASSWORD_TOO_SHORT", msg, nil)
	case errors.Is(err, auth.ErrBadRole):
		writeErr(w, http.StatusBadRequest, "ROLE_INVALID", msg, nil)
	case errors.Is(err, auth.ErrOwnerFixed), errors.Is(err, auth.ErrOneOwner):
		writeErr(w, http.StatusBadRequest, "OWNER_FIXED", msg, nil)
	case errors.Is(err, auth.ErrBadName):
		writeErr(w, http.StatusBadRequest, "NAME_INVALID", msg, nil)
	case errors.Is(err, auth.ErrNoPerson):
		writeErr(w, http.StatusNotFound, "PERSON_NOT_FOUND", msg, nil)
	case errors.Is(err, auth.ErrBadEmail):
		writeErr(w, http.StatusBadRequest, "EMAIL_INVALID", msg, nil)
	case errors.Is(err, auth.ErrEmailTaken):
		writeErr(w, http.StatusConflict, "EMAIL_TAKEN", msg, nil)
	default:
		writeErr(w, http.StatusInternalServerError, "INTERNAL_ERROR", msg, nil)
	}
}

func (s *Server) handleAcceptInvite(w http.ResponseWriter, r *http.Request) {
	if !s.signInReady(w) {
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return
	}
	token := mux.Vars(r)["token"]
	ctx := r.Context()
	id, kind, err := s.deps.Invites.Peek(ctx, token)
	if err != nil {
		writeErr(w, http.StatusGone, "INVITE_INVALID", err.Error(), nil)
		return
	}
	person, err := s.deps.People.Active(ctx, id)
	if err != nil {
		writeErr(w, http.StatusGone, "INVITE_INVALID", auth.ErrNoInvite.Error(), nil)
		return
	}
	// The choice is checked before the link is spent, so a taken username
	// or a short password can be fixed and tried again.
	username := body.Username
	if kind == auth.InviteReset {
		username = person.Username
	}
	if err := s.deps.People.CheckSignIn(ctx, id, username, body.Password); err != nil {
		peopleError(w, err)
		return
	}
	if _, _, err := s.deps.Invites.Use(ctx, token); err != nil {
		writeErr(w, http.StatusGone, "INVITE_INVALID", err.Error(), nil)
		return
	}
	if err := s.deps.People.SetSignIn(ctx, id, username, body.Password); err != nil {
		peopleError(w, err)
		return
	}
	// A new password signs the person out everywhere else.
	_ = s.deps.Sessions.DeleteFor(ctx, id)
	person, _ = s.deps.People.Active(ctx, id)
	if err := s.setSession(w, r, person); err != nil {
		writeErr(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, auth.Principal{Person: person, Via: auth.ViaSession})
}

// mayManage reports whether actor may change people with role: the Owner
// changes anyone but themselves into another role; an Admin changes
// Members and Visitors.
func mayManage(actor auth.Person, role auth.Role) bool {
	switch actor.Role {
	case auth.RoleOwner:
		return role != auth.RoleOwner
	case auth.RoleAdmin:
		return role == auth.RoleMember || role == auth.RoleVisitor
	}
	return false
}

func (s *Server) handleListPeople(w http.ResponseWriter, r *http.Request) {
	if !s.signInReady(w) {
		return
	}
	list, err := s.deps.People.List(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, filterPeople(list, r))
}

// linkView is a one-time link to give a person: the path of its page on
// this Toskar, such as /invite/abc…, and when it stops working.
type linkView struct {
	Path      string    `json:"path"`
	Kind      string    `json:"kind"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s *Server) makeLink(r *http.Request, person auth.Person) (linkView, error) {
	kind := auth.InviteNew
	if person.SignIn {
		kind = auth.InviteReset
	}
	token, expires, err := s.deps.Invites.Create(r.Context(), person.ID, kind, auth.PersonID(r.Context()))
	if err != nil {
		return linkView{}, err
	}
	return linkView{Path: "/invite/" + token, Kind: kind, ExpiresAt: expires}, nil
}

func (s *Server) handleAddPerson(w http.ResponseWriter, r *http.Request) {
	if !s.signInReady(w) {
		return
	}
	var body newPerson
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return
	}
	if body.Role == "" {
		body.Role = auth.RoleMember
	}
	if !mayManage(auth.PrincipalFrom(r.Context()).Person, body.Role) {
		writeErr(w, http.StatusForbidden, "ROLE_REQUIRED", "only the Owner adds Admins", map[string]any{"role": string(auth.RoleOwner)})
		return
	}
	// A password needs a username to sign in with; both are checked before
	// anyone is added, so a bad one adds no one (#592).
	if body.Password != "" {
		if err := s.deps.People.CheckSignIn(r.Context(), "", body.Username, body.Password); err != nil {
			peopleError(w, err)
			return
		}
	}
	person, err := s.deps.People.CreateWith(r.Context(), body.Name, body.Role, body.Email)
	if err != nil {
		peopleError(w, err)
		return
	}
	s.record(r.Context(), person, "added", "role "+string(person.Role))
	if body.Password != "" {
		password := body.Password
		if err := s.setPassword(r.Context(), person, passwordChange{Password: &password, Username: body.Username, MustChange: body.MustChange}); err != nil {
			peopleError(w, err)
			return
		}
		person, _ = s.deps.People.Get(r.Context(), person.ID)
		writeJSON(w, http.StatusCreated, map[string]any{"person": person})
		return
	}
	link, err := s.makeLink(r, person)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"person": person, "link": link})
}

func (s *Server) handleChangePerson(w http.ResponseWriter, r *http.Request) {
	if !s.signInReady(w) {
		return
	}
	var body struct {
		auth.Change
		passwordChange
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return
	}
	change := body.Change
	actor := auth.PrincipalFrom(r.Context()).Person
	target, err := s.deps.People.Get(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		peopleError(w, err)
		return
	}
	self := target.ID == actor.ID
	switch {
	case self && (change.Role != nil || change.Disabled != nil):
		// Nobody demotes or disables themselves out of administering.
		writeErr(w, http.StatusForbidden, "OWNER_FIXED", "you can't change your own role or disable yourself", nil)
		return
	case !self && !mayManage(actor, target.Role):
		writeErr(w, http.StatusForbidden, "ROLE_REQUIRED", "only the Owner changes Admins", map[string]any{"role": string(auth.RoleOwner)})
		return
	case change.Role != nil && !mayManage(actor, *change.Role) && *change.Role != target.Role:
		writeErr(w, http.StatusForbidden, "ROLE_REQUIRED", "only the Owner makes Admins", map[string]any{"role": string(auth.RoleOwner)})
		return
	}
	person, err := s.deps.People.Update(r.Context(), target.ID, change)
	if err != nil {
		peopleError(w, err)
		return
	}
	// Disabled: signed out everywhere, and their keys stop working.
	if person.Disabled != nil {
		_ = s.deps.Sessions.DeleteFor(r.Context(), person.ID)
	}
	s.recordChange(r.Context(), target, person)
	if body.Password != nil {
		if err := s.setPassword(r.Context(), person, body.passwordChange); err != nil {
			peopleError(w, err)
			return
		}
		person, _ = s.deps.People.Get(r.Context(), person.ID)
	}
	writeJSON(w, http.StatusOK, person)
}

func (s *Server) handlePersonLink(w http.ResponseWriter, r *http.Request) {
	if !s.signInReady(w) {
		return
	}
	actor := auth.PrincipalFrom(r.Context()).Person
	target, err := s.deps.People.Active(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		peopleError(w, err)
		return
	}
	if target.ID != actor.ID && !mayManage(actor, target.Role) {
		writeErr(w, http.StatusForbidden, "ROLE_REQUIRED", "only the Owner sends Admins a link", map[string]any{"role": string(auth.RoleOwner)})
		return
	}
	link, err := s.makeLink(r, target)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	s.record(r.Context(), target, "link_made", link.Kind)
	writeJSON(w, http.StatusCreated, link)
}
