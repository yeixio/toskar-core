package models

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/yeixio/toskar-core/internal/gguf/gguftest"
)

// Models other apps downloaded are found where each app keeps them, and an
// Ollama blob, which has no .gguf name, can be added in place (#467).
func TestFindOtherApps(t *testing.T) {
	m, _, dir := importManager(t)
	home := filepath.Join(dir, "home")
	m.Home = home
	t.Setenv("OLLAMA_MODELS", "")
	ctx := context.Background()

	lm := filepath.Join(home, ".lmstudio", "models", "lmstudio-community", "Qwen3-8B-GGUF")
	gguftest.Write(t, lm, "Qwen3-8B-Q4_K_M.gguf", gguftest.Options{Name: "Qwen3 8B", FileType: 15})
	gguftest.Write(t, lm, "mmproj-Qwen3-8B-F16.gguf", gguftest.Options{Architecture: "clip", Type: "mmproj"})
	gguftest.Write(t, lm, "broken.gguf", gguftest.Options{Cut: 20})

	ollama := filepath.Join(home, ".ollama", "models")
	blob := gguftest.Write(t, filepath.Join(ollama, "blobs"), "sha256-abc123", gguftest.Options{Name: "Llama 3.2 3B Instruct"})
	manifest := filepath.Join(ollama, "manifests", "registry.ollama.ai", "library", "llama3.2", "3b")
	if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
		t.Fatal(err)
	}
	projBlob := gguftest.Write(t, filepath.Join(ollama, "blobs"), "sha256-proj9", gguftest.Options{Architecture: "clip", Type: "mmproj"})
	_ = os.WriteFile(manifest, []byte(`{"layers":[{"mediaType":"application/vnd.ollama.image.model","digest":"sha256:abc123"},{"mediaType":"application/vnd.ollama.image.projector","digest":"sha256:proj9"},{"mediaType":"application/vnd.ollama.image.license","digest":"sha256:def"}]}`), 0o644)

	var llamacpp string
	for _, d := range otherAppDirs(home, runtime.GOOS, os.Getenv) {
		if d.app == "llamacpp" {
			llamacpp = d.dir
		}
	}
	gguftest.Write(t, llamacpp, "bartowski_Phi-4-mini-Q8_0.gguf", gguftest.Options{})

	found := m.FindOtherApps(ctx)
	got := map[string]FoundModel{}
	for _, f := range found {
		got[f.App+":"+f.Name] = f
	}
	if len(found) != 3 {
		t.Fatalf("found %d: %+v", len(found), found)
	}
	q, o, p := got["lmstudio:Qwen3 8B"], got["ollama:llama3.2:3b"], got["llamacpp:bartowski_Phi-4-mini-Q8_0"]
	if q.Quantization != "Q4_K_M" || o.Path != blob || o.Projector != projBlob || p.Quantization != "Q8_0" || q.Projector == "" {
		t.Fatalf("found = %+v", found)
	}

	// The blob is added in place, by the path the list gave; then it's marked.
	imported, err := m.ImportFile(ctx, ImportRequest{Path: o.Path, InPlace: true, DisplayName: o.Name})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range m.FindOtherApps(ctx) {
		if f.Path == blob && f.ModelID != imported.ID {
			t.Errorf("after adding: %+v, want model %s", f, imported.ID)
		}
	}
	// Its projector blob, which has no .gguf name either, can be paired.
	if err := m.SetProjector(ctx, imported.ID, o.Projector); err != nil || !m.SeesImages(imported.ID) {
		t.Errorf("pairing Ollama's projector: %v, sees images %v", err, m.SeesImages(imported.ID))
	}
	// A file without a .gguf name that no app lists is still refused.
	stray := gguftest.Write(t, dir, "stray-blob", gguftest.Options{})
	if _, err := m.ImportFile(ctx, ImportRequest{Path: stray, InPlace: true}); err == nil {
		t.Error("added a file no app lists without a .gguf name")
	}
	if ollamaName("/m", "/m/registry.ollama.ai/me/custom/latest") != "me/custom:latest" {
		t.Error("namespaced Ollama names")
	}
}

// A GGUF put in Toskar's own models folder by hand is found, and added
// where it is rather than copied beside itself; Toskar's own models aren't
// listed (#763).
func TestFindModelsFolder(t *testing.T) {
	m, storage, dir := importManager(t)
	m.Home = filepath.Join(dir, "home")
	t.Setenv("OLLAMA_MODELS", "")
	ctx := context.Background()

	folder := storage.ModelsDir()
	loose := gguftest.Write(t, filepath.Join(folder, "mine"), "Gemma-3-1B-Q4_K_M.gguf", gguftest.Options{Name: "Gemma 3 1B", FileType: 15})
	gguftest.Write(t, folder, "Gemma-3-1B.mmproj.gguf", gguftest.Options{Architecture: "clip", Type: "mmproj"})
	gguftest.Write(t, filepath.Join(folder, ".partial"), "upload-1.gguf", gguftest.Options{})

	found := m.FindOtherApps(ctx)
	if len(found) != 1 || found[0].App != "toskar" || found[0].Path != loose || found[0].Name != "Gemma 3 1B" {
		t.Fatalf("found = %+v", found)
	}

	// Asked for a copy, it's still used where it is.
	imported, err := m.ImportFile(ctx, ImportRequest{Path: loose})
	if err != nil || imported.Copying {
		t.Fatalf("import = %+v, %v", imported, err)
	}
	if ok, at, _ := storage.IsInstalled(ctx, imported.ID); !ok || at != loose {
		t.Fatalf("installed %v at %s, want %s", ok, at, loose)
	}
	if entries, _ := os.ReadDir(folder); len(entries) != 3 {
		t.Errorf("models folder has %d entries, want the same 3 (no copy)", len(entries))
	}
	// Now a model, it's on the Models page, not offered again.
	if found := m.FindOtherApps(ctx); len(found) != 0 {
		t.Errorf("after adding, found = %+v", found)
	}
}
