package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/comfyui"
)

// ComfyUIs is a ComfyUI an Admin connected and the workflows saved for it
// (#958). Every route needs the Admin role: custom nodes run code on that
// computer, so only an Admin decides what Toskar calls.
type ComfyUIs interface {
	ComfyUI(ctx context.Context) (ComfyUIStatus, error)
	SetComfyUI(ctx context.Context, in ComfyUIInput) (ComfyUIStatus, error)
	ComfyWorkflows(ctx context.Context) ([]ComfyWorkflow, error)
	DetectComfyWorkflow(ctx context.Context, graph json.RawMessage) (comfyui.Inputs, error)
	SaveComfyWorkflow(ctx context.Context, in ComfyWorkflowInput) (ComfyWorkflow, error)
	DeleteComfyWorkflow(ctx context.Context, name string) error
}

// ComfyUIStatus is the ComfyUI's address and what a check found.
type ComfyUIStatus struct {
	BaseURL string `json:"base_url"`
	// HasToken reports a stored token, never its value.
	HasToken bool `json:"has_token"`
	// Info is what the check found; Error is why it failed.
	Info  *comfyui.Info `json:"info,omitempty"`
	Error string        `json:"error,omitempty"`
}

// ComfyUIInput changes it. An empty Token keeps the stored one.
type ComfyUIInput struct {
	BaseURL    string `json:"base_url"`
	Token      string `json:"token,omitempty"`
	ClearToken bool   `json:"clear_token,omitempty"`
}

// ComfyWorkflow is a saved workflow without its graph.
type ComfyWorkflow struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Inputs      comfyui.Inputs `json:"inputs"`
	Nodes       int            `json:"nodes"`
}

// ComfyWorkflowInput saves one. Without inputs, they are detected.
type ComfyWorkflowInput struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Graph       json.RawMessage `json:"graph"`
	Inputs      *comfyui.Inputs `json:"inputs,omitempty"`
}

// BindComfyUI attaches the ComfyUI routes.
func (s *Server) BindComfyUI(c ComfyUIs) { s.comfyui = c }

func (s *Server) comfyuiRoutes(api *mux.Router) {
	h := func(f http.HandlerFunc) http.HandlerFunc {
		return s.atLeast(auth.RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
			if s.comfyui == nil {
				writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "ComfyUI is not available.", nil)
				return
			}
			f(w, r)
		})
	}
	api.HandleFunc("/comfyui", h(s.handleGetComfyUI)).Methods(http.MethodGet)
	api.HandleFunc("/comfyui", h(s.handlePutComfyUI)).Methods(http.MethodPut)
	api.HandleFunc("/comfyui/workflows", h(s.handleListComfyWorkflows)).Methods(http.MethodGet)
	api.HandleFunc("/comfyui/workflows", h(s.handleSaveComfyWorkflow)).Methods(http.MethodPost)
	api.HandleFunc("/comfyui/workflows/detect", h(s.handleDetectComfyWorkflow)).Methods(http.MethodPost)
	api.HandleFunc("/comfyui/workflows/{name}", h(s.handleDeleteComfyWorkflow)).Methods(http.MethodDelete)
}

func (s *Server) handleGetComfyUI(w http.ResponseWriter, r *http.Request) {
	out, err := s.comfyui.ComfyUI(r.Context())
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "COMFYUI_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePutComfyUI(w http.ResponseWriter, r *http.Request) {
	var in ComfyUIInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "COMFYUI_BAD_URL", "The body must be {\"base_url\", \"token\"}.", nil)
		return
	}
	out, err := s.comfyui.SetComfyUI(r.Context(), in)
	if err != nil {
		writeErrFrom(w, http.StatusBadRequest, "COMFYUI_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleListComfyWorkflows(w http.ResponseWriter, r *http.Request) {
	out, err := s.comfyui.ComfyWorkflows(r.Context())
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "COMFYUI_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workflows": out})
}

// A workflow's graph can be large: a few MB covers any exported one.
const comfyWorkflowLimit = 8 << 20

func (s *Server) handleDetectComfyWorkflow(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Graph json.RawMessage `json:"graph"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, comfyWorkflowLimit)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "COMFYUI_BAD_WORKFLOW", "The body must be {\"graph\"}, the workflow exported in API format.", nil)
		return
	}
	out, err := s.comfyui.DetectComfyWorkflow(r.Context(), in.Graph)
	if err != nil {
		writeErrFrom(w, http.StatusBadRequest, "COMFYUI_BAD_WORKFLOW", err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSaveComfyWorkflow(w http.ResponseWriter, r *http.Request) {
	var in ComfyWorkflowInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, comfyWorkflowLimit)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "COMFYUI_BAD_WORKFLOW", "The body must be {\"name\", \"graph\"}, with the workflow exported in API format.", nil)
		return
	}
	out, err := s.comfyui.SaveComfyWorkflow(r.Context(), in)
	if err != nil {
		writeErrFrom(w, http.StatusBadRequest, "COMFYUI_BAD_WORKFLOW", err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDeleteComfyWorkflow(w http.ResponseWriter, r *http.Request) {
	if err := s.comfyui.DeleteComfyWorkflow(r.Context(), mux.Vars(r)["name"]); err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "COMFYUI_FAILED", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
