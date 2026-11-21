package api

import (
	"net/http"

	"github.com/yeixio/toskar-core/internal/voice"
)

// handleRealtime is live voice mode (#795): a WebSocket that speaks a
// subset of OpenAI's Realtime protocol (docs/voice.md). The person talks,
// Toskar answers out loud through the normal chat path, and they can talk
// over it.
func (s *Server) handleRealtime(w http.ResponseWriter, r *http.Request) {
	if s.deps.Voice == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Live voice isn't available.", nil)
		return
	}
	parts, err := s.deps.Voice(r.Context())
	if err != nil {
		writeErrFrom(w, http.StatusServiceUnavailable, "VOICE_UNAVAILABLE", err)
		return
	}
	if r.Header.Get("Upgrade") == "" {
		writeErr(w, http.StatusUpgradeRequired, "WEBSOCKET_REQUIRED", "live voice is a WebSocket", nil)
		return
	}
	// The session outlives the request's handlers, not its connection: it
	// ends when the client goes.
	_ = voice.Serve(w, r, parts)
}
