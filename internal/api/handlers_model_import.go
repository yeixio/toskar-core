package api

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/models"
)

// modelImportBody is POST /models/import as JSON: a file on this computer.
type modelImportBody struct {
	Path        string   `json:"path"`
	InPlace     bool     `json:"in_place"`
	ID          string   `json:"id"`
	DisplayName string   `json:"display_name"`
	Tags        []string `json:"tags"`
}

// handleImportModel adds a GGUF model from a file (#467): a path on this
// computer as JSON, copied or used in place, or the file itself as the
// body (application/octet-stream), from the web app on another device or
// a paired computer. The file is checked before it's added.
func (s *Server) handleImportModel(w http.ResponseWriter, r *http.Request) {
	// New setup waits for the Unlock once Pro's trial has ended (#336).
	if s.refuseLocked(w) {
		return
	}
	if s.deps.ImportModel == nil || s.deps.AdoptModelUpload == nil || s.deps.ModelUploadPath == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Adding a model from a file is not available.", nil)
		return
	}
	media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	var (
		got models.Imported
		err error
	)
	if media == "application/json" {
		var body modelImportBody
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON.", nil)
			return
		}
		got, err = s.deps.ImportModel(r.Context(), models.ImportRequest{
			Path: body.Path, InPlace: body.InPlace, ID: body.ID, DisplayName: body.DisplayName, Tags: body.Tags,
		})
	} else {
		got, err = s.importUpload(r)
	}
	if err != nil {
		writeErrFrom(w, http.StatusBadRequest, "MODEL_IMPORT_FAILED", err)
		return
	}
	d := got.Details
	writeJSON(w, http.StatusOK, map[string]any{
		"model_id":  got.ID,
		"projector": got.Projector,
		"status":    map[bool]string{true: "copying", false: "installed"}[got.Copying],
		"details": map[string]any{
			"name":           d.Name,
			"architecture":   d.Architecture,
			"parameters":     d.Parameters,
			"size_label":     d.SizeLabel,
			"context_length": d.ContextLength,
			"quantization":   d.Quantization,
			"chat_template":  d.ChatTemplate,
			"license":        d.License,
		},
	})
}

// handleUpdateAddedModel renames or retags a model added from a file or a
// link (#467).
func (s *Server) handleUpdateAddedModel(w http.ResponseWriter, r *http.Request) {
	if s.deps.UpdateAddedModel == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Editing a model is not available.", nil)
		return
	}
	var body struct {
		DisplayName *string  `json:"display_name"`
		Tags        []string `json:"tags"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON.", nil)
		return
	}
	entry, err := s.deps.UpdateAddedModel(r.Context(), mux.Vars(r)["id"], body.DisplayName, body.Tags)
	if err != nil {
		writeErrFrom(w, http.StatusBadRequest, "MODEL_UPDATE_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"model_id": entry.ID, "display_name": entry.DisplayName, "tags": entry.Tags})
}

// handleSetModelProjector gives a model its vision projector, a GGUF file on
// this computer (#467).
func (s *Server) handleSetModelProjector(w http.ResponseWriter, r *http.Request) {
	if s.deps.SetModelProjector == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Adding a projector is not available.", nil)
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON.", nil)
		return
	}
	if err := s.deps.SetModelProjector(r.Context(), mux.Vars(r)["id"], body.Path); err != nil {
		writeErrFrom(w, http.StatusBadRequest, "MODEL_UPDATE_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"model_id": mux.Vars(r)["id"], "vision": true})
}

// handleFoundModels lists models other local AI apps keep on this computer,
// to add without downloading them again (#467).
func (s *Server) handleFoundModels(w http.ResponseWriter, r *http.Request) {
	found := []models.FoundModel{}
	if s.deps.FindModelsInOtherApps != nil {
		if list := s.deps.FindModelsInOtherApps(r.Context()); list != nil {
			found = list
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": found})
}

// importUpload writes the body to the models folder, then adds it.
// filename, display_name, and id come in the query.
func (s *Server) importUpload(r *http.Request) (models.Imported, error) {
	q := r.URL.Query()
	name := q.Get("filename")
	if !strings.HasSuffix(strings.ToLower(name), ".gguf") {
		return models.Imported{}, errors.New("send the file's name, ending in .gguf, as filename")
	}
	path, err := s.deps.ModelUploadPath()
	if err != nil {
		return models.Imported{}, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return models.Imported{}, err
	}
	_, err = io.Copy(f, r.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(path)
		return models.Imported{}, err
	}
	got, err := s.deps.AdoptModelUpload(r.Context(), models.ImportRequest{
		Path: name, ID: q.Get("id"), DisplayName: q.Get("display_name"),
	}, path)
	if err != nil {
		_ = os.Remove(path)
	}
	return got, err
}
