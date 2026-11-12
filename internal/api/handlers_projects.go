package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/mimir"
	"github.com/yeixio/toskar-core/internal/projects"
	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// ProjectService is projects as the API sees them (#1136): the person's
// own, as their chats are.
type ProjectService interface {
	List(ctx context.Context) ([]contracts.Project, error)
	Get(ctx context.Context, id string) (contracts.Project, error)
	Create(ctx context.Context, f projects.Fields) (contracts.Project, error)
	Update(ctx context.Context, id string, f projects.Fields) (contracts.Project, error)
	Delete(ctx context.Context, id string, deleteChats bool) error
	AddFile(ctx context.Context, id, name string, content []byte) (contracts.Project, error)
	RemoveFile(ctx context.Context, id, name string) error
}

// BindProjects attaches the project routes.
func (s *Server) BindProjects(p ProjectService) { s.projects = p }

func (s *Server) projectRoutes(api *mux.Router) {
	api.HandleFunc("/projects", s.projectsReady(s.handleListProjects)).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/projects", s.projectsReady(s.handleCreateProject)).Methods(http.MethodPost)
	api.HandleFunc("/projects/{id}", s.projectsReady(s.handleGetProject)).Methods(http.MethodGet, http.MethodOptions)
	api.HandleFunc("/projects/{id}", s.projectsReady(s.handleUpdateProject)).Methods(http.MethodPatch)
	api.HandleFunc("/projects/{id}", s.projectsReady(s.handleDeleteProject)).Methods(http.MethodDelete)
	api.HandleFunc("/projects/{id}/files", s.projectsReady(s.handleAddProjectFile)).Methods(http.MethodPost)
	api.HandleFunc("/projects/{id}/files/{name}", s.projectsReady(s.handleRemoveProjectFile)).Methods(http.MethodDelete)
}

func (s *Server) projectsReady(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.projects == nil {
			writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Projects are not available.", nil)
			return
		}
		h(w, r)
	}
}

func writeProjectErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, projects.ErrNotFound):
		writeErrFrom(w, http.StatusNotFound, "PROJECT_NOT_FOUND", err)
	case errors.Is(err, mimir.ErrProjectFileNotFound):
		writeErrFrom(w, http.StatusNotFound, "PROJECT_FILE_NOT_FOUND", err)
	case errors.Is(err, projects.ErrNoFiles):
		writeErrFrom(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", err)
	default:
		writeErrFrom(w, http.StatusBadRequest, "BAD_REQUEST", err)
	}
}

// projectInput is a project's fields in a request; one left out stays as
// it is.
type projectInput struct {
	Name         *string `json:"name"`
	Instructions *string `json:"instructions"`
	// Folder is the full path of a folder on this computer for the
	// project's chats to work in, or "" for none. Admins only.
	Folder    *string   `json:"folder"`
	ProfileID *string   `json:"profile_id"`
	ModelID   *string   `json:"model_id"`
	MemoryOff *bool     `json:"memory_off"`
	Knowledge *[]string `json:"knowledge"`
}

// projectFields checks what the caller may set and returns it for the store, or
// writes why not.
func projectFields(w http.ResponseWriter, r *http.Request) (projects.Fields, bool) {
	var in projectInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be JSON.", nil)
		return projects.Fields{}, false
	}
	role := auth.PrincipalFrom(r.Context()).Person.Role
	f := projects.Fields{Name: in.Name, Instructions: in.Instructions, ProfileID: in.ProfileID, ModelID: in.ModelID, MemoryOff: in.MemoryOff}
	if in.Folder != nil {
		// A folder lets the project's chats run things on this computer,
		// as a chat's own does.
		if !role.AtLeast(auth.RoleAdmin) {
			writeErr(w, http.StatusForbidden, "ROLE_REQUIRED", "choosing a folder for a project needs the admin role", map[string]any{"role": "admin"})
			return projects.Fields{}, false
		}
		dir := ""
		if *in.Folder != "" {
			if dir = turnopts.Project(*in.Folder); dir == "" {
				writeErr(w, http.StatusBadRequest, "NOT_A_FOLDER", "There's no folder at that path on this computer. Use its full path.", nil)
				return projects.Fields{}, false
			}
		}
		f.Folder = &dir
	}
	if in.Knowledge != nil {
		// Knowledge sources are for Members and up to read, so linking one
		// is too.
		if len(*in.Knowledge) > 0 && !role.AtLeast(auth.RoleMember) {
			writeErr(w, http.StatusForbidden, "ROLE_REQUIRED", "linking knowledge to a project needs the member role", map[string]any{"role": "member"})
			return projects.Fields{}, false
		}
		f.Knowledge, f.SetKnowledge = *in.Knowledge, true
	}
	return f, true
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	list, err := s.projects.List(r.Context())
	if err != nil {
		writeErrFrom(w, http.StatusInternalServerError, "INTERNAL_ERROR", err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	f, ok := projectFields(w, r)
	if !ok {
		return
	}
	p, err := s.projects.Create(r.Context(), f)
	if err != nil {
		writeProjectErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	p, err := s.projects.Get(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeProjectErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	f, ok := projectFields(w, r)
	if !ok {
		return
	}
	p, err := s.projects.Update(r.Context(), mux.Vars(r)["id"], f)
	if err != nil {
		writeProjectErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// handleDeleteProject deletes a project with its files and memories. Its
// chats move out of it, unless ?chats=delete.
func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	chats := r.URL.Query().Get("chats")
	if chats != "" && chats != "keep" && chats != "delete" {
		writeErr(w, http.StatusBadRequest, "BAD_REQUEST", `chats is "keep" or "delete"`, map[string]any{"detail": `chats is "keep" or "delete"`})
		return
	}
	if err := s.projects.Delete(r.Context(), mux.Vars(r)["id"], chats == "delete"); err != nil {
		writeProjectErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// projectFile is a file added to a project: text, or another file's bytes
// in base64, as a knowledge upload is.
type projectFile struct {
	Filename      string `json:"filename"`
	Text          string `json:"text,omitempty"`
	ContentBase64 string `json:"content_base64,omitempty"`
}

func (s *Server) handleAddProjectFile(w http.ResponseWriter, r *http.Request) {
	var in projectFile
	r.Body = http.MaxBytesReader(w, r.Body, mimir.MaxTextBytes*4/3+1<<20)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be JSON.", nil)
		return
	}
	content := []byte(in.Text)
	if in.ContentBase64 != "" {
		var err error
		if content, err = base64.StdEncoding.DecodeString(in.ContentBase64); err != nil {
			writeErr(w, http.StatusBadRequest, "BAD_REQUEST", "the upload is not valid base64", map[string]any{"detail": "the upload is not valid base64"})
			return
		}
	}
	p, err := s.projects.AddFile(r.Context(), mux.Vars(r)["id"], in.Filename, content)
	if err != nil {
		writeProjectErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleRemoveProjectFile(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	if err := s.projects.RemoveFile(r.Context(), vars["id"], vars["name"]); err != nil {
		writeProjectErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// checkProjectID writes why a chat can't be put in a project, the caller's
// own: "" is none.
func (s *Server) checkProjectID(w http.ResponseWriter, r *http.Request, id string) bool {
	if id == "" {
		return true
	}
	if s.projects == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Projects are not available.", nil)
		return false
	}
	if _, err := s.projects.Get(r.Context(), id); err != nil {
		writeProjectErr(w, err)
		return false
	}
	return true
}
