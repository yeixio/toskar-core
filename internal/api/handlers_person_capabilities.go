package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/imagegen"
)

// What each role and person may use (#589): Admins set it, and routes that
// need a capability refuse with CAPABILITY_OFF when it's off. (The
// capability inventory, what this computer can do, is /capabilities.)

func (s *Server) personCapabilityRoutes(api *mux.Router) {
	api.HandleFunc("/roles/capabilities", s.atLeast(auth.RoleAdmin, s.handleRoleCapabilities)).Methods(http.MethodGet)
	api.HandleFunc("/roles/{role}/capabilities", s.atLeast(auth.RoleAdmin, s.handleSetRoleCapabilities)).Methods(http.MethodPut)
	api.HandleFunc("/people/{id}/capabilities", s.atLeast(auth.RoleAdmin, s.handlePersonCapabilities)).Methods(http.MethodGet)
	api.HandleFunc("/people/{id}/capabilities", s.atLeast(auth.RoleAdmin, s.handleSetPersonCapabilities)).Methods(http.MethodPut)
}

// routeCapability is what a route needs besides its role.
var routeCapability = map[string]auth.Capability{
	"POST /api/v1/speech/transcribe":              auth.CapVoice,
	"POST /api/v1/speech":                         auth.CapVoice,
	"POST /api/v1/automations":                    auth.CapAutomations,
	"POST /api/v1/automations/preview":            auth.CapAutomations,
	"POST /api/v1/automations/parse":              auth.CapAutomations,
	"POST /api/v1/knowledge/sources":              auth.CapKnowledgeAdd,
	"PATCH /api/v1/knowledge/sources/{id}":        auth.CapKnowledgeAdd,
	"DELETE /api/v1/knowledge/sources/{id}":       auth.CapKnowledgeAdd,
	"POST /api/v1/knowledge/sources/{id}/refresh": auth.CapKnowledgeAdd,
	"POST /api/v1/knowledge/search":               auth.CapKnowledge,
	"GET /api/v1/remote-access/route":             auth.CapRemoteAccess,
}

// capabilitiesOf is what person may use; everything when people aren't
// kept.
func (s *Server) capabilitiesOf(ctx context.Context, person auth.Person) auth.Capabilities {
	if s.deps.People == nil {
		return auth.DefaultCapabilities(auth.RoleOwner)
	}
	caps, err := s.deps.People.CapabilitiesOf(ctx, person)
	if err != nil {
		return auth.DefaultCapabilities(person.Role)
	}
	return caps
}

// refuseCapability writes CAPABILITY_OFF when c is off for person, naming
// the Owner, who can turn it on, and reports whether it did.
func (s *Server) refuseCapability(w http.ResponseWriter, ctx context.Context, person auth.Person, c auth.Capability) bool {
	if s.capabilitiesOf(ctx, person).Allows(c) {
		return false
	}
	admin := "Owner"
	if s.deps.People != nil {
		if owner, err := s.deps.People.Get(ctx, auth.OwnerID); err == nil {
			admin = owner.Name
		}
	}
	writeErr(w, http.StatusForbidden, "CAPABILITY_OFF", string(c)+" is turned off for your account; ask an Admin to turn it on",
		map[string]any{"capability": string(c), "admin": admin})
	return true
}

// uploadCapability is what uploading a file named name needs: pictures and
// videos to look at, a recording to speak a message, or files.
func uploadCapability(name string) auth.Capability {
	switch {
	case imagegen.IsEditable(name) || artifacts.IsVideo(name):
		return auth.CapPicturesIn
	case artifacts.IsAudio(name):
		return auth.CapVoice
	}
	return auth.CapFiles
}

type roleCapabilitiesView struct {
	// Capabilities are every capability, in the order to show them.
	Capabilities []auth.Capability `json:"capabilities"`
	// Roles are each role's, and Defaults what each role ships with.
	Roles    map[auth.Role]auth.Capabilities `json:"roles"`
	Defaults map[auth.Role]auth.Capabilities `json:"defaults"`
}

func (s *Server) handleRoleCapabilities(w http.ResponseWriter, r *http.Request) {
	if !s.signInReady(w) {
		return
	}
	roles, err := s.deps.People.RoleCapabilities(r.Context())
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "INTERNAL_ERROR", err)
		return
	}
	defaults := map[auth.Role]auth.Capabilities{}
	for role := range roles {
		defaults[role] = auth.DefaultCapabilities(role)
	}
	writeJSON(w, http.StatusOK, roleCapabilitiesView{Capabilities: auth.AllCapabilities, Roles: roles, Defaults: defaults})
}

// handleSetRoleCapabilities sets a role's capabilities; only the Owner
// changes the Admins'.
func (s *Server) handleSetRoleCapabilities(w http.ResponseWriter, r *http.Request) {
	if !s.signInReady(w) {
		return
	}
	role := auth.Role(mux.Vars(r)["role"])
	actor := auth.PrincipalFrom(r.Context()).Person
	if role == auth.RoleAdmin && actor.Role != auth.RoleOwner {
		writeErr(w, http.StatusForbidden, "ROLE_REQUIRED", "only the Owner changes what Admins may use", map[string]any{"role": string(auth.RoleOwner)})
		return
	}
	var caps map[auth.Capability]bool
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&caps); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return
	}
	if err := s.deps.People.SetRoleCapabilities(r.Context(), role, caps); err != nil {
		capabilityError(w, err)
		return
	}
	roles, err := s.deps.People.RoleCapabilities(r.Context())
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "INTERNAL_ERROR", err)
		return
	}
	writeJSON(w, http.StatusOK, roles[role])
}

type personCapabilitiesView struct {
	// Set are the Allows (true) and Blocks (false) set for them alone;
	// anything else is as their role's.
	Set map[auth.Capability]bool `json:"set"`
	// Effective is what they may use now.
	Effective auth.Capabilities `json:"effective"`
}

func (s *Server) personCapabilitiesView(ctx context.Context, person auth.Person) (personCapabilitiesView, error) {
	set, err := s.deps.People.PersonCapabilities(ctx, person.ID)
	if err != nil {
		return personCapabilitiesView{}, err
	}
	return personCapabilitiesView{Set: set, Effective: s.capabilitiesOf(ctx, person)}, nil
}

func (s *Server) handlePersonCapabilities(w http.ResponseWriter, r *http.Request) {
	target, ok := s.managed(w, r)
	if !ok {
		return
	}
	view, err := s.personCapabilitiesView(r.Context(), target)
	if err != nil {
		capabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// handleSetPersonCapabilities sets one person's Allows and Blocks: true,
// false, or null for as their role's.
func (s *Server) handleSetPersonCapabilities(w http.ResponseWriter, r *http.Request) {
	target, ok := s.managed(w, r)
	if !ok {
		return
	}
	var set map[auth.Capability]*bool
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&set); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return
	}
	if err := s.deps.People.SetPersonCapabilities(r.Context(), target.ID, set); err != nil {
		capabilityError(w, err)
		return
	}
	s.record(r.Context(), target, "capabilities_changed", "")
	view, err := s.personCapabilitiesView(r.Context(), target)
	if err != nil {
		capabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func capabilityError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrCapabilityRole):
		writeErr(w, http.StatusBadRequest, "OWNER_FIXED", err.Error(), nil)
	case errors.Is(err, auth.ErrCapabilityUnknown):
		writeErr(w, http.StatusBadRequest, "CAPABILITY_UNKNOWN", err.Error(), nil)
	default:
		peopleError(w, err)
	}
}
