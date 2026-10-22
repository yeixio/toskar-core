package models

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/yeixio/toskar-core/internal/gguf"
)

// FoundModel is a GGUF model another local AI app already downloaded on
// this computer, which can be added without downloading it again (#467).
type FoundModel struct {
	// App is lmstudio, ollama, llamacpp, or gpt4all; toskar is a file put
	// in Toskar's own models folder by hand (#763).
	App string `json:"app"`
	// Path is the file; an Ollama model is its blob, named by digest.
	Path string `json:"path"`
	// Name is what the app calls it (Ollama's model:tag), or the header's
	// name, or the file's.
	Name         string `json:"name"`
	SizeBytes    uint64 `json:"size_bytes"`
	Architecture string `json:"architecture,omitempty"`
	Parameters   string `json:"parameters,omitempty"`
	Quantization string `json:"quantization,omitempty"`
	// ModelID is set when the file is already a model here.
	ModelID string `json:"model_id,omitempty"`
	// Projector is its vision projector, when the app keeps one with it.
	Projector string `json:"projector,omitempty"`
}

// appDir is a folder where an app keeps its models.
type appDir struct {
	app, dir string
	ollama   bool
}

// otherAppDirs are where LM Studio, Ollama, llama.cpp, and GPT4All keep
// models for a home folder on goos. OLLAMA_MODELS moves Ollama's.
func otherAppDirs(home, goos string, env func(string) string) []appDir {
	ollama := env("OLLAMA_MODELS")
	if ollama == "" {
		ollama = filepath.Join(home, ".ollama", "models")
	}
	dirs := []appDir{
		{app: "lmstudio", dir: filepath.Join(home, ".lmstudio", "models")},
		{app: "lmstudio", dir: filepath.Join(home, ".cache", "lm-studio", "models")},
		{app: "ollama", dir: ollama, ollama: true},
	}
	switch goos {
	case "darwin":
		dirs = append(dirs,
			appDir{app: "llamacpp", dir: filepath.Join(home, "Library", "Caches", "llama.cpp")},
			appDir{app: "gpt4all", dir: filepath.Join(home, "Library", "Application Support", "nomic.ai", "GPT4All")})
	case "windows":
		local := env("LOCALAPPDATA")
		if local == "" {
			local = filepath.Join(home, "AppData", "Local")
		}
		dirs = append(dirs,
			appDir{app: "llamacpp", dir: filepath.Join(local, "llama.cpp")},
			appDir{app: "gpt4all", dir: filepath.Join(local, "nomic.ai", "GPT4All")})
	default:
		cache := env("XDG_CACHE_HOME")
		if cache == "" {
			cache = filepath.Join(home, ".cache")
		}
		data := env("XDG_DATA_HOME")
		if data == "" {
			data = filepath.Join(home, ".local", "share")
		}
		dirs = append(dirs,
			appDir{app: "llamacpp", dir: filepath.Join(cache, "llama.cpp")},
			appDir{app: "gpt4all", dir: filepath.Join(data, "nomic.ai", "GPT4All")})
	}
	return dirs
}

// FindOtherApps lists the GGUF models other local AI apps keep on this
// computer, and files put in Toskar's own models folder by hand that
// aren't models yet (#763). It only reads: headers, and Ollama's
// manifests. A file that isn't a whole model, or is a vision projector, is
// left out.
func (m *Manager) FindOtherApps(ctx context.Context) []FoundModel {
	home := m.Home
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if home == "" {
		return nil
	}
	installed, _ := m.storage.ListInstalled(ctx)
	byPath := map[string]string{}
	for id, p := range installed {
		byPath[canonical(p)] = id
	}
	var out []FoundModel
	seen := map[string]bool{}
	add := func(app, path, name, projector string) {
		key := canonical(path)
		if seen[key] || ctx.Err() != nil {
			return
		}
		seen[key] = true
		st, err := os.Stat(path)
		if err != nil || !st.Mode().IsRegular() {
			return
		}
		d, err := gguf.Inspect(path)
		if err != nil || d.Projector() {
			return
		}
		if name == "" {
			name = d.Name
		}
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		}
		params := d.SizeLabel
		if params == "" && d.Parameters > 0 {
			params = parameterLabel(d.Parameters)
		}
		out = append(out, FoundModel{
			App: app, Path: path, Name: name, SizeBytes: uint64(st.Size()),
			Architecture: d.Architecture, Parameters: params, Quantization: d.Quantization,
			ModelID: byPath[key], Projector: projector,
		})
	}
	dirs := otherAppDirs(home, runtime.GOOS, os.Getenv)
	if folder := m.storage.ModelsDir(); folder != "" {
		dirs = append([]appDir{{app: "toskar", dir: folder}}, dirs...)
	}
	for _, dir := range dirs {
		if dir.ollama {
			for _, o := range ollamaModels(dir.dir) {
				add(dir.app, o.path, o.name, o.projector)
			}
			continue
		}
		_ = filepath.WalkDir(dir.dir, func(path string, e fs.DirEntry, err error) error {
			if err != nil {
				if path == dir.dir {
					return fs.SkipDir
				}
				return nil
			}
			if e.IsDir() && strings.HasPrefix(e.Name(), ".") && path != dir.dir {
				return fs.SkipDir
			}
			if !e.IsDir() && strings.EqualFold(filepath.Ext(path), ".gguf") && !strings.Contains(strings.ToLower(e.Name()), "mmproj") {
				// Toskar's own models are already listed on the Models page.
				if dir.app == "toskar" && byPath[canonical(path)] != "" {
					return nil
				}
				add(dir.app, path, "", projectorNear(path))
			}
			return nil
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].App != out[j].App {
			return out[i].App < out[j].App
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// foundProjector reports a projector FindOtherApps lists, such as an
// Ollama blob, which has no .gguf name.
func (m *Manager) foundProjector(ctx context.Context, path string) bool {
	want := canonical(path)
	for _, f := range m.FindOtherApps(ctx) {
		if f.Projector != "" && canonical(f.Projector) == want {
			return true
		}
	}
	return false
}

// foundPath reports a path FindOtherApps lists, so a file without a .gguf
// name, such as an Ollama blob, can be added.
func (m *Manager) foundPath(ctx context.Context, path string) bool {
	want := canonical(path)
	for _, f := range m.FindOtherApps(ctx) {
		if canonical(f.Path) == want {
			return true
		}
	}
	return false
}

type ollamaModel struct{ name, path, projector string }

// ollamaModels reads Ollama's manifests: each model:tag names its weights
// as a blob by digest.
func ollamaModels(dir string) []ollamaModel {
	root := filepath.Join(dir, "manifests")
	var out []ollamaModel
	_ = filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			if err != nil && path == root {
				return fs.SkipDir
			}
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil || len(raw) > 1<<20 {
			return nil
		}
		var manifest struct {
			Layers []struct {
				MediaType string `json:"mediaType"`
				Digest    string `json:"digest"`
			} `json:"layers"`
		}
		if json.Unmarshal(raw, &manifest) != nil {
			return nil
		}
		var model ollamaModel
		for _, l := range manifest.Layers {
			hex, ok := strings.CutPrefix(l.Digest, "sha256:")
			if !ok || strings.ContainsAny(hex, `/\.`) {
				continue
			}
			blob := filepath.Join(dir, "blobs", "sha256-"+hex)
			switch l.MediaType {
			case "application/vnd.ollama.image.model":
				model.path = blob
			case "application/vnd.ollama.image.projector":
				model.projector = blob
			}
		}
		if model.path != "" {
			model.name = ollamaName(root, path)
			out = append(out, model)
		}
		return nil
	})
	return out
}

// ollamaName is a manifest's model:tag: registry.ollama.ai/library/qwen3/8b
// is qwen3:8b, and another namespace stays, as in ns/model:tag.
func ollamaName(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 3 {
		return ""
	}
	parts = parts[1:] // the registry
	if parts[0] == "library" {
		parts = parts[1:]
	}
	tag := parts[len(parts)-1]
	return strings.Join(parts[:len(parts)-1], "/") + ":" + tag
}

// canonical is a path with links resolved, to compare files.
func canonical(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}
