package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/swarm"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// SwarmService is swarms as the API sees them (#1135): the person's own, as
// their chats are. Another person's reads as not found.
type SwarmService interface {
	// Start starts a swarm in the background and returns its record, with
	// its agents once they're planned.
	Start(ctx context.Context, in swarm.Start) (contracts.Swarm, error)
	Get(ctx context.Context, id string) (contracts.Swarm, error)
	// List is the person's swarms in a project, or all of theirs for "".
	List(ctx context.Context, projectID string) ([]contracts.Swarm, error)
	// Stop stops every agent of a swarm, and reports whether any was running.
	Stop(ctx context.Context, id string) (bool, error)
	Delete(ctx context.Context, id string) error
}

// ErrConversationNotFound is a chat to run a swarm in that isn't the
// person's.
var ErrConversationNotFound = contracts.NewError("NOT_FOUND", map[string]any{"detail": "conversation not found"}, errors.New("conversation not found"))

// BindSwarms attaches the swarm routes.
func (s *Server) BindSwarms(sw SwarmService) { s.swarms = sw }

func (s *Server) swarmRoutes(api *mux.Router) {
	api.HandleFunc("/swarms", s.swarmsReady(s.handleListSwarms)).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/swarms", s.swarmsReady(s.handleStartSwarm)).Methods(http.MethodPost)
	api.HandleFunc("/swarms/{id}", s.swarmsReady(s.handleGetSwarm)).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/swarms/{id}", s.swarmsReady(s.handleDeleteSwarm)).Methods(http.MethodDelete)
	api.HandleFunc("/swarms/{id}/stop", s.swarmsReady(s.handleStopSwarm)).Methods(http.MethodPost)
}

func (s *Server) swarmsReady(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.swarms == nil {
			writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Swarms are not available.", nil)
			return
		}
		h(w, r)
	}
}

func writeSwarmErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, swarm.ErrNotFound):
		writeErrFrom(w, http.StatusNotFound, "SWARM_NOT_FOUND", err)
	case errors.Is(err, swarm.ErrRunning):
		writeErrFrom(w, http.StatusConflict, "SWARM_RUNNING", err)
	case errors.Is(err, ErrConversationNotFound):
		writeErrFrom(w, http.StatusNotFound, "NOT_FOUND", err)
	default:
		writeErrFrom(w, http.StatusBadRequest, "BAD_REQUEST", err)
	}
}

// swarmStart is POST /swarms's body.
type swarmStart struct {
	Prompt string `json:"prompt"`
	// Mode is split or race; empty is the profile's, or split.
	Mode           string `json:"mode"`
	ProjectID      string `json:"project_id"`
	ConversationID string `json:"conversation_id"`
	// Budget overrides the profile's agents, tokens, minutes, and a race's
	// models for this swarm.
	Budget *contracts.SwarmPolicy `json:"budget"`
}

// refuseSwarm writes why the caller can't start a swarm: Members and up
// can, and portals' guests can't (docs/features/swarms.md).
func refuseSwarm(w http.ResponseWriter, r *http.Request) bool {
	p := auth.PrincipalFrom(r.Context()).Person
	if p.PortalID != "" {
		writeErr(w, http.StatusForbidden, "PORTAL_MEMBERS", "this chat is for the people who sign in to this Toskar", nil)
		return true
	}
	if !p.Role.AtLeast(auth.RoleMember) {
		writeErr(w, http.StatusForbidden, "ROLE_REQUIRED", "starting a swarm needs the member role", map[string]any{"role": "member"})
		return true
	}
	return false
}

// checkSwarmMode writes why a swarm mode isn't one, "" being the default.
func checkSwarmMode(w http.ResponseWriter, mode string) bool {
	if mode != "" && !swarm.ValidMode(mode) {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", `swarm is "split" or "race"`, map[string]any{"detail": `swarm is "split" or "race"`})
		return false
	}
	return true
}

func (s *Server) handleStartSwarm(w http.ResponseWriter, r *http.Request) {
	if refuseSwarm(w, r) {
		return
	}
	var in swarmStart
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be JSON.", nil)
		return
	}
	if strings.TrimSpace(in.Prompt) == "" {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "prompt is required", map[string]any{"detail": "prompt is required"})
		return
	}
	if !checkSwarmMode(w, in.Mode) {
		return
	}
	if err := profiles.ValidateSwarm(in.Budget); err != nil {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", strings.Replace(err.Error(), "orchestration.swarm.", "budget.", 1), map[string]any{"detail": err.Error()})
		return
	}
	if !s.checkProjectID(w, r, in.ProjectID) {
		return
	}
	sw, err := s.swarms.Start(r.Context(), swarm.Start{Prompt: in.Prompt, Mode: in.Mode, ProjectID: in.ProjectID,
		ConversationID: in.ConversationID, Budget: in.Budget})
	if err != nil {
		writeSwarmErr(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, sw)
}

func (s *Server) handleListSwarms(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	if !s.checkProjectID(w, r, projectID) {
		return
	}
	list, err := s.swarms.List(r.Context(), projectID)
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "INTERNAL_ERROR", err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGetSwarm(w http.ResponseWriter, r *http.Request) {
	sw, err := s.swarms.Get(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeSwarmErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sw)
}

// handleStopSwarm stops every agent of a swarm. What they wrote is kept in
// their chats, and the coordinator's turn stops with them.
func (s *Server) handleStopSwarm(w http.ResponseWriter, r *http.Request) {
	stopped, err := s.swarms.Stop(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeSwarmErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, chatStopped{Stopped: stopped})
}

// handleDeleteSwarm deletes a finished swarm's record. Its chats stay, as
// ordinary chats in its project.
func (s *Server) handleDeleteSwarm(w http.ResponseWriter, r *http.Request) {
	if err := s.swarms.Delete(r.Context(), mux.Vars(r)["id"]); err != nil {
		writeSwarmErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
