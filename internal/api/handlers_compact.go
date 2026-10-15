package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/muninn"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Auto-compact (#342): a long chat's summary, which the person reads and
// corrects, compacting now, and a new chat from the summary.

func (s *Server) compactRoutes(api *mux.Router) {
	api.HandleFunc("/conversations/{id}/summary", s.handleConversationSummary).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/conversations/{id}/summary", s.handleEditConversationSummary).Methods(http.MethodPut)
	api.HandleFunc("/conversations/{id}/compact", s.handleCompactConversation).Methods(http.MethodPost)
	api.HandleFunc("/conversations/{id}/from-summary", s.handleConversationFromSummary).Methods(http.MethodPost)
}

// Compacter is auto-compact, from the app.
type Compacter struct {
	Summary func(ctx context.Context, conversationID string) (muninn.Summary, error)
	Edit    func(ctx context.Context, conversationID, text string) (muninn.Summary, error)
	Now     func(ctx context.Context, conversationID string) (muninn.Summary, error)
	NewChat func(ctx context.Context, conversationID string) (contracts.Conversation, error)
	// SetOff keeps a chat verbatim, or lets it be compacted again.
	SetOff func(ctx context.Context, conversationID string, off bool) error
}

func (s *Server) compactReady(w http.ResponseWriter) bool {
	if s.deps.Compact == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Auto-compact isn't available.", nil)
		return false
	}
	return true
}

// compactStatus is the HTTP status for an auto-compact error's code.
func compactStatus(err error) int {
	switch code, _ := contracts.ErrorCode(err); code {
	case "SUMMARY_NOT_FOUND":
		return http.StatusNotFound
	case "NOTHING_TO_COMPACT", "SUMMARY_EMPTY":
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func (s *Server) handleConversationSummary(w http.ResponseWriter, r *http.Request) {
	if !s.compactReady(w) {
		return
	}
	sum, err := s.deps.Compact.Summary(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeErrFrom(w, compactStatus(err), "SUMMARY_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, sum)
}

func (s *Server) handleEditConversationSummary(w http.ResponseWriter, r *http.Request) {
	if !s.compactReady(w) {
		return
	}
	var body struct {
		Summary string `json:"summary"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return
	}
	sum, err := s.deps.Compact.Edit(r.Context(), mux.Vars(r)["id"], body.Summary)
	if err != nil {
		writeErrFrom(w, compactStatus(err), "SUMMARY_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, sum)
}

func (s *Server) handleCompactConversation(w http.ResponseWriter, r *http.Request) {
	if !s.compactReady(w) {
		return
	}
	sum, err := s.deps.Compact.Now(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeErrFrom(w, compactStatus(err), "COMPACT_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, sum)
}

func (s *Server) handleConversationFromSummary(w http.ResponseWriter, r *http.Request) {
	if !s.compactReady(w) {
		return
	}
	conv, err := s.deps.Compact.NewChat(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeErrFrom(w, compactStatus(err), "SUMMARY_FAILED", err)
		return
	}
	writeJSON(w, http.StatusCreated, conv)
}
