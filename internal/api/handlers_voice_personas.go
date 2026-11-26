package api

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/speech"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Voice personas (#1319): everyone lists them and picks their own; Admins
// make, change, and delete them.
func (s *Server) voicePersonaRoutes(api *mux.Router) {
	api.HandleFunc("/voice/personas", s.handleVoicePersonas).Methods(http.MethodGet)
	api.HandleFunc("/voice/persona", s.handlePickVoicePersona).Methods(http.MethodPut)
	api.HandleFunc("/voice/personas", s.atLeast(auth.RoleAdmin, s.handleCreateVoicePersona)).Methods(http.MethodPost)
	api.HandleFunc("/voice/personas/{id}", s.atLeast(auth.RoleAdmin, s.handleUpdateVoicePersona)).Methods(http.MethodPut)
	api.HandleFunc("/voice/personas/{id}", s.atLeast(auth.RoleAdmin, s.handleDeleteVoicePersona)).Methods(http.MethodDelete)
}

func (s *Server) personasReady(w http.ResponseWriter) bool {
	if s.deps.VoicePersonas == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Voice personas aren't available.", nil)
		return false
	}
	return true
}

// handleVoicePersonas answers the personas and the caller's pick.
func (s *Server) handleVoicePersonas(w http.ResponseWriter, r *http.Request) {
	if !s.personasReady(w) {
		return
	}
	list, err := s.deps.VoicePersonas.List(r.Context())
	if err != nil {
		writeErrFrom(w, http.StatusServiceUnavailable, "SPEECH_UNAVAILABLE", err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handlePickVoicePersona has the caller read aloud in a persona, or in
// each language's own voice with an empty id.
func (s *Server) handlePickVoicePersona(w http.ResponseWriter, r *http.Request) {
	if !s.personasReady(w) {
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return
	}
	if err := s.deps.VoicePersonas.Pick(r.Context(), body.ID); err != nil {
		s.personaError(w, err)
		return
	}
	s.handleVoicePersonas(w, r)
}

func (s *Server) readPersona(w http.ResponseWriter, r *http.Request) (speech.Persona, bool) {
	var p speech.Persona
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return p, false
	}
	return p, true
}

func (s *Server) handleCreateVoicePersona(w http.ResponseWriter, r *http.Request) {
	if !s.personasReady(w) {
		return
	}
	p, ok := s.readPersona(w, r)
	if !ok {
		return
	}
	if _, err := s.deps.VoicePersonas.Create(r.Context(), p); err != nil {
		s.personaError(w, err)
		return
	}
	s.handleVoicePersonas(w, r)
}

func (s *Server) handleUpdateVoicePersona(w http.ResponseWriter, r *http.Request) {
	if !s.personasReady(w) {
		return
	}
	p, ok := s.readPersona(w, r)
	if !ok {
		return
	}
	if _, err := s.deps.VoicePersonas.Update(r.Context(), mux.Vars(r)["id"], p); err != nil {
		s.personaError(w, err)
		return
	}
	s.handleVoicePersonas(w, r)
}

func (s *Server) handleDeleteVoicePersona(w http.ResponseWriter, r *http.Request) {
	if !s.personasReady(w) {
		return
	}
	if err := s.deps.VoicePersonas.Delete(r.Context(), mux.Vars(r)["id"]); err != nil {
		s.personaError(w, err)
		return
	}
	s.handleVoicePersonas(w, r)
}

// personaError answers a persona that wasn't found with 404 and the rest
// with 400, under their own codes.
func (s *Server) personaError(w http.ResponseWriter, err error) {
	if code, _ := contracts.ErrorCode(err); code == "PERSONA_NOT_FOUND" {
		writeErrFrom(w, http.StatusNotFound, code, err)
		return
	}
	writeErrFrom(w, http.StatusBadRequest, "SPEECH_UNAVAILABLE", err)
}
