package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/remotetools"
	"github.com/yeixio/toskar-core/internal/tools"
)

func (s *Server) handleListTools(w http.ResponseWriter, r *http.Request) {
	if s.deps.ListTools == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Tools not available.", nil)
		return
	}
	list, err := s.deps.ListTools(r.Context())
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "TOOLS_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleToolProviders lists each computer's tool providers and their state
// (Gungnir §16).
func (s *Server) handleToolProviders(w http.ResponseWriter, r *http.Request) {
	if s.deps.ToolProviders == nil {
		writeJSON(w, http.StatusOK, []remotetools.NodeProviders{})
		return
	}
	writeJSON(w, http.StatusOK, s.deps.ToolProviders(r.Context()))
}

func (s *Server) handleToolActivity(w http.ResponseWriter, r *http.Request) {
	if s.deps.ToolActivity == nil {
		writeJSON(w, http.StatusOK, []tools.Activity{})
		return
	}
	writeJSON(w, http.StatusOK, s.deps.ToolActivity())
}

func (s *Server) handleSetToolEnabled(w http.ResponseWriter, r *http.Request) {
	if s.deps.SetToolEnabled == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Tools not available.", nil)
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return
	}
	id := mux.Vars(r)["id"]
	if err := s.deps.SetToolEnabled(r.Context(), id, body.Enabled); err != nil {
		writeErrFrom(w, http.StatusBadRequest, "TOOL_UPDATE_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, toolEnabled{ID: id, Enabled: body.Enabled})
}

func (s *Server) handleTestTool(w http.ResponseWriter, r *http.Request) {
	if s.deps.TestTool == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Tools not available.", nil)
		return
	}
	var body struct {
		Args map[string]any `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid body", nil)
		return
	}
	result, err := s.deps.TestTool(r.Context(), mux.Vars(r)["id"], body.Args)
	if err != nil {
		writeErrFrom(w, http.StatusBadRequest, "TOOL_TEST_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleDescribeTool returns one tool's descriptor (Gungnir §7–8).
func (s *Server) handleDescribeTool(w http.ResponseWriter, r *http.Request) {
	if s.deps.DescribeTool == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Tools not available.", nil)
		return
	}
	d, err := s.deps.DescribeTool(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeErrFrom(w, http.StatusNotFound, "NOT_FOUND", err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// handleToolRuns lists audited tool calls, newest first (Gungnir §13).
func (s *Server) handleToolRuns(w http.ResponseWriter, r *http.Request) {
	if s.deps.ListToolRuns == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Tools not available.", nil)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	runs, err := s.deps.ListToolRuns(r.Context(), r.URL.Query().Get("tool_id"), r.URL.Query().Get("conversation_id"), limit)
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "TOOLS_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

// toolEnabled is a tool turned on or off.
type toolEnabled struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}
