package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/speech"
	"github.com/yeixio/toskar-core/internal/voice"
)

// BindSpeech attaches Read aloud (Gungnir §19).
func (s *Server) BindSpeech(engine *speech.Engine, store *artifacts.Store) {
	s.speech, s.speechStore = engine, store
}

func (s *Server) speechRoutes(api *mux.Router) {
	api.HandleFunc("/speech", s.handleSpeech).Methods(http.MethodPost)
	api.HandleFunc("/speech/transcribe", s.handleTranscribe).Methods(http.MethodPost)
	api.HandleFunc("/speech/say", s.handleSay).Methods(http.MethodPost)
	api.HandleFunc("/voice/realtime", s.handleRealtime).Methods(http.MethodGet)
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
	writeJSON(w, http.StatusOK, spokenText{Artifact: a, Seconds: seconds})
}

// spokenText is text read aloud: the recording, and how long it plays.
type spokenText struct {
	Artifact artifacts.Artifact `json:"artifact"`
	Seconds  float64            `json:"seconds"`
}

// Transcription is a recording's text, for the person to check before
// sending, with the language it was in and how long it was.
type Transcription struct {
	Text            string  `json:"text"`
	Language        string  `json:"language"`
	DurationSeconds float64 `json:"duration_seconds"`
}

// maxSayRunes is the most handleSay reads at once: a sentence or a few,
// as a client speaking an answer as it arrives sends them.
const maxSayRunes = 1000

// handleSay reads a little text aloud in live voice mode's voice (#795):
// Kokoro, natural, where it speaks the language, and Piper elsewhere, kept
// loaded between requests. It answers the audio itself, a WAV file, and
// keeps nothing, so a phone, the car, or the TV can speak an answer a
// sentence at a time in the voice the computer uses, over plain HTTPS,
// through the relay too.
func (s *Server) handleSay(w http.ResponseWriter, r *http.Request) {
	if s.deps.Voice == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Speech isn't available.", nil)
		return
	}
	var body struct {
		Text string `json:"text"`
		// Voice is a Kokoro or Piper voice; empty is the language's own.
		Voice string `json:"voice"`
		// Language is the text's, such as en or pt-BR; empty detects it.
		Language string `json:"language"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "text is required", nil)
		return
	}
	if utf8.RuneCountInString(text) > maxSayRunes {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "text is longer than "+strconv.Itoa(maxSayRunes)+" characters; send a sentence or a few at a time", nil)
		return
	}
	parts, err := s.deps.Voice(r.Context())
	if err != nil {
		writeErrFrom(w, http.StatusServiceUnavailable, "SPEECH_UNAVAILABLE", err)
		return
	}
	samples, err := parts.Speaker.Speak(r.Context(), text, body.Voice, body.Language)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		writeErrFrom(w, http.StatusBadRequest, "SPEECH_FAILED", err)
		return
	}
	wav := voice.WAV(samples, voice.SampleRate)
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Content-Length", strconv.Itoa(len(wav)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(wav)
}
