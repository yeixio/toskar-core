package imagegen

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	rootmanifests "github.com/yeixio/toskar-core/manifests"
)

// The picture and clip models setup offers are listed in
// manifests/models/media.json (#551), built into Toskar. A media.json in
// the data folder's manifests/models replaces it, as catalog.json does for
// chat models, so a model can be added without a new release.

// mediaManifest is media.json.
type mediaManifest struct {
	Images []manifestModel `json:"images"`
	Video  []manifestModel `json:"video"`
}

// manifestModel is a model as media.json lists it, with the sampling
// settings Model keeps out of its JSON.
type manifestModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Note says why the model is set up as it is, for whoever edits the
	// manifest; it isn't shown.
	Note           string  `json:"note,omitempty"`
	Kind           string  `json:"kind"`
	Tier           string  `json:"tier,omitempty"`
	Description    string  `json:"description"`
	License        string  `json:"license"`
	MemoryBytes    int64   `json:"memory_bytes"`
	GraphicsBytes  int64   `json:"graphics_bytes,omitempty"`
	Edits          bool    `json:"edits,omitempty"`
	EditOnly       bool    `json:"edit_only,omitempty"`
	Untested       bool    `json:"untested,omitempty"`
	Steps          int     `json:"steps"`
	CFG            float64 `json:"cfg"`
	FlowShift      float64 `json:"flow_shift,omitempty"`
	Frames         int     `json:"frames,omitempty"`
	FPS            int     `json:"fps,omitempty"`
	HighNoiseSteps int     `json:"high_noise_steps,omitempty"`
	HighNoiseCFG   float64 `json:"high_noise_cfg,omitempty"`
	ModelArgs      string  `json:"model_args,omitempty"`
	NeedsLicense   bool    `json:"needs_license,omitempty"`
	LicenseURL     string  `json:"license_url,omitempty"`
	LicenseAllows  string  `json:"license_allows,omitempty"`
	Files          []File  `json:"files"`
	// Stages are its measured parts (#550); see Stage.
	Stages []Stage `json:"stages,omitempty"`
}

func (m manifestModel) model() Model {
	out := Model{ID: m.ID, Name: m.Name, Kind: m.Kind, Tier: m.Tier, Description: m.Description, License: m.License,
		MemoryBytes: m.MemoryBytes, GraphicsBytes: m.GraphicsBytes, Edits: m.Edits, EditOnly: m.EditOnly, Untested: m.Untested,
		Steps: m.Steps, CFG: m.CFG, FlowShift: m.FlowShift, Frames: m.Frames, FPS: m.FPS,
		HighNoiseSteps: m.HighNoiseSteps, HighNoiseCFG: m.HighNoiseCFG, ModelArgs: m.ModelArgs,
		NeedsLicense: m.NeedsLicense, LicenseURL: m.LicenseURL, LicenseAllows: m.LicenseAllows,
		Files: append([]File(nil), m.Files...), Stages: append([]Stage(nil), m.Stages...)}
	// Measured parts say what the default request needs (#550).
	if len(out.Stages) > 0 {
		out.GraphicsBytes = out.GraphicsFor(out.defaultRequest())
	}
	return out
}

var (
	manifestMu sync.RWMutex
	loaded     *mediaManifest
)

// parseManifest reads media.json and checks every file is pinned.
func parseManifest(raw []byte) (*mediaManifest, error) {
	var m mediaManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("media.json: %w", err)
	}
	if len(m.Images) == 0 || len(m.Video) == 0 {
		return nil, fmt.Errorf("media.json lists no image or no video models")
	}
	for _, list := range [][]manifestModel{m.Images, m.Video} {
		for _, model := range list {
			if model.ID == "" || model.Steps <= 0 || len(model.Files) == 0 {
				return nil, fmt.Errorf("media.json: model %q is incomplete", model.ID)
			}
			if model.NeedsLicense && (model.LicenseURL == "" || model.LicenseAllows == "") {
				return nil, fmt.Errorf("media.json: %s needs a license but doesn't link or describe it", model.ID)
			}
			for _, f := range model.Files {
				if f.Gated && !model.NeedsLicense {
					return nil, fmt.Errorf("media.json: %s has a gated file but doesn't need a license", model.ID)
				}
				if f.Role == "" || f.Size <= 0 || len(f.SHA256) != 64 || (f.Source == "" && (f.Repo == "" || f.Revision == "")) {
					return nil, fmt.Errorf("media.json: %s's %s is not pinned", model.ID, f.Path)
				}
			}
		}
	}
	return &m, nil
}

// manifest is the media.json in use: one loaded with UseManifest, or the
// one built in.
func manifest() *mediaManifest {
	manifestMu.RLock()
	m := loaded
	manifestMu.RUnlock()
	if m != nil {
		return m
	}
	manifestMu.Lock()
	defer manifestMu.Unlock()
	if loaded == nil {
		m, err := parseManifest(rootmanifests.MediaJSON)
		if err != nil {
			// The built-in manifest is checked by its tests.
			panic(err)
		}
		loaded = m
	}
	return loaded
}

// UseManifest replaces the built-in media.json with the one at path. A
// file that doesn't read or isn't fully pinned is refused, and the
// built-in one stays.
func UseManifest(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	m, err := parseManifest(raw)
	if err != nil {
		return err
	}
	manifestMu.Lock()
	loaded = m
	manifestMu.Unlock()
	return nil
}

func toModels(list []manifestModel) []Model {
	out := make([]Model, 0, len(list))
	for _, m := range list {
		out = append(out, m.model())
	}
	return out
}
