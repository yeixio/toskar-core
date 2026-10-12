package api

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/speech"
)

// BindSpeech attaches Read aloud (Gungnir §19).
func (s *Server) BindSpeech(engine *speech.Engine, store *artifacts.Store) {
	s.speech, s.speechStore = engine, store
}

func (s *Server) speechRoutes(api *mux.Router) {
	api.HandleFunc("/speech", s.handleSpeech).Methods(http.MethodPost)
	api.HandleFunc("/speech/transcribe", s.handleTranscribe).Methods(http.MethodPost)
}

// handleTranscribe turns a recording the person attached into text, for
// speaking a message instead of typing it (yeixio/toskar-apps#155). It runs
// speech.transcribe (Whisper) wherever it's set up, this computer or a
// paired one, and answers the text for the person to check before sending.
func (s *Server) handleTranscribe(w http.ResponseWriter, r *http.Request) {
	if s.deps.Transcribe == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Transcription is not available.", nil)
		return
	}
	var body struct {
		// ArtifactID is the recording, added with POST /artifacts.
		ArtifactID string `json:"artifact_id"`
		// Language is a hint, such as en or de; empty detects it.
		Language string `json:"language"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ArtifactID == "" {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "artifact_id is required", nil)
		return
	}
	out, err := s.deps.Transcribe(r.Context(), body.ArtifactID, body.Language)
	if err != nil {
		writeErrFrom(w, http.StatusServiceUnavailable, "SPEECH_UNAVAILABLE", err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSpeech reads text aloud on this computer and keeps the audio as a
// file in the chat, so the app can play it.
func (s *Server) handleSpeech(w http.ResponseWriter, r *http.Request) {
	if s.speech == nil || s.speechStore == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Read aloud is not available.", nil)
		return
	}
	if ok, why := s.speech.Available(); !ok {
		writeErr(w, http.StatusServiceUnavailable, "SPEECH_UNAVAILABLE", why, nil)
		return
	}
	var body struct {
		Text           string `json:"text"`
		Voice          string `json:"voice"`
		ConversationID string `json:"conversation_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return
	}
	ctx := artifacts.WithConversation(r.Context(), body.ConversationID)
	a, seconds, err := speech.SaveSpeech(ctx, s.speech, s.speechStore, body.Text, body.Voice, "read-aloud")
	if err != nil {
		writeErrFrom(w, http.StatusBadRequest, "SPEECH_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"artifact": a, "seconds": seconds})
}
