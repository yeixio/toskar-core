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
	writeJSON(w, http.StatusOK, importedModel{
		ModelID:   got.ID,
		Projector: got.Projector,
		Status:    map[bool]string{true: "copying", false: "installed"}[got.Copying],
		Details: modelFileDetails{
			Name: d.Name, Architecture: d.Architecture, Parameters: d.Parameters, SizeLabel: d.SizeLabel,
			ContextLength: d.ContextLength, Quantization: d.Quantization, ChatTemplate: d.ChatTemplate, License: d.License,
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
	writeJSON(w, http.StatusOK, addedModelView{ModelID: entry.ID, DisplayName: entry.DisplayName, Tags: entry.Tags})
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
	writeJSON(w, http.StatusOK, modelVision{ModelID: mux.Vars(r)["id"], Vision: true})
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
	writeJSON(w, http.StatusOK, foundModels{Models: found})
}

// importedModel is a model added from a file (#467): copying while its
// file is still being copied in, then installed.
type importedModel struct {
	ModelID string `json:"model_id"`
	// Projector is the vision projector found beside it, if any.
	Projector string           `json:"projector"`
	Status    string           `json:"status"`
	Details   modelFileDetails `json:"details"`
}

// modelFileDetails is what a GGUF file says about its model.
type modelFileDetails struct {
	Name          string `json:"name"`
	Architecture  string `json:"architecture"`
	Parameters    uint64 `json:"parameters"`
	SizeLabel     string `json:"size_label"`
	ContextLength uint64 `json:"context_length"`
	Quantization  string `json:"quantization"`
	// ChatTemplate says the file has its own chat template.
	ChatTemplate bool   `json:"chat_template"`
	License      string `json:"license"`
}

// foundModels are model files other apps on this computer have (#467).
type foundModels struct {
	Models []models.FoundModel `json:"models"`
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

// addedModelView is an added model's name and tags, after a change.
type addedModelView struct {
	ModelID     string   `json:"model_id"`
	DisplayName string   `json:"display_name"`
	Tags        []string `json:"tags"`
}

// modelVision says a model sees pictures now, with its projector set.
type modelVision struct {
	ModelID string `json:"model_id"`
	Vision  bool   `json:"vision"`
}

// handleSetModelSpeculation turns speculative decoding Auto or Off for a
// chat model on this computer (#835). A loaded model changes when it next
// loads.
func (s *Server) handleSetModelSpeculation(w http.ResponseWriter, r *http.Request) {
	if s.deps.SetModelSpeculation == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Changing speculation is not available.", nil)
		return
	}
	var body struct {
		Speculation string `json:"speculation"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON.", nil)
		return
	}
	id := mux.Vars(r)["id"]
	if err := s.deps.SetModelSpeculation(r.Context(), id, body.Speculation); err != nil {
		writeErrFrom(w, http.StatusBadRequest, "MODEL_UPDATE_FAILED", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"model_id": id, "speculation": body.Speculation})
}
