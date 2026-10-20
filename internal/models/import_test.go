package models

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/gguf/gguftest"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

func importManager(t *testing.T) (*Manager, *Storage, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	storage := NewStorage(db.SQL, filepath.Join(dir, "models"))
	bus := events.NewBus(64)
	return NewManager(&Catalog{}, storage, NewDownloader(storage, bus, db.SQL), bus), storage, dir
}

func waitInstalled(t *testing.T, storage *Storage, id string) string {
	t.Helper()
	for range 200 {
		if ok, path, _ := storage.IsInstalled(context.Background(), id); ok {
			return path
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s never installed", id)
	return ""
}

// A GGUF file is added as a model: copied by default, named from its
// header, and the copy deleted with the model while the original stays
// (#467).
func TestImportFileCopies(t *testing.T) {
	m, storage, dir := importManager(t)
	ctx := context.Background()
	src := gguftest.Write(t, filepath.Join(dir, "elsewhere"), "Tiny-Llama.Q4_K_M.gguf", gguftest.Options{Name: "Tiny Llama", FileType: 15, Context: 4096})
	got, err := m.ImportFile(ctx, ImportRequest{Path: src})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "tiny-llama-q4-k-m" || !got.Copying || got.Details.ContextLength != 4096 {
		t.Fatalf("imported = %+v", got)
	}
	path := waitInstalled(t, storage, got.ID)
	if path != storage.ModelPath(got.ID) {
		t.Errorf("installed at %s, want the models folder", path)
	}
	entry, _ := m.catalog.Get(got.ID)
	if entry.DisplayName != "Tiny Llama" || entry.Variant != "Q4_K_M" || entry.Context != 4096 || entry.Family != "llama" {
		t.Errorf("entry = %+v", entry)
	}
	if err := m.Delete(ctx, got.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the copy wasn't deleted")
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("the original is gone: %v", err)
	}
}

// Used in place, the model is the file itself, and deleting the model
// leaves the file: it isn't Toskar's.
func TestImportFileInPlace(t *testing.T) {
	m, storage, dir := importManager(t)
	ctx := context.Background()
	src := gguftest.Write(t, filepath.Join(dir, "lmstudio"), "model.gguf", gguftest.Options{Name: "Kept Where It Is"})
	got, err := m.ImportFile(ctx, ImportRequest{Path: src, InPlace: true, DisplayName: "Mine"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Copying || waitInstalled(t, storage, got.ID) != src {
		t.Fatalf("imported = %+v", got)
	}
	if entry, _ := m.catalog.Get(got.ID); entry.DisplayName != "Mine" {
		t.Errorf("name = %q", entry.DisplayName)
	}
	// The same file again keeps its id; another file with the same name gets its own.
	again, _ := m.ImportFile(ctx, ImportRequest{Path: src, InPlace: true, DisplayName: "Mine"})
	other := gguftest.Write(t, filepath.Join(dir, "other"), "model.gguf", gguftest.Options{})
	second, _ := m.ImportFile(ctx, ImportRequest{Path: other, InPlace: true, DisplayName: "Mine"})
	if again.ID != got.ID || second.ID != got.ID+"-2" {
		t.Errorf("ids: %s, %s, %s", got.ID, again.ID, second.ID)
	}
	if err := m.Delete(ctx, got.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("deleting the model deleted the file: %v", err)
	}
}

// What can't be added is refused before anything is recorded.
func TestImportFileRefuses(t *testing.T) {
	m, _, dir := importManager(t)
	ctx := context.Background()
	text := filepath.Join(dir, "notes.gguf")
	_ = os.WriteFile(text, []byte("not a model"), 0o644)
	for name, req := range map[string]ImportRequest{
		"relative":  {Path: "model.gguf"},
		"extension": {Path: gguftest.Write(t, dir, "model.bin", gguftest.Options{})},
		"missing":   {Path: filepath.Join(dir, "nope.gguf")},
		"truncated": {Path: gguftest.Write(t, dir, "cut.gguf", gguftest.Options{Cut: 10})},
		"not gguf":  {Path: text},
		"projector": {Path: gguftest.Write(t, dir, "mmproj.gguf", gguftest.Options{Architecture: "clip", Type: "mmproj"})},
	} {
		_, err := m.ImportFile(ctx, req)
		if code, _ := contracts.ErrorCode(err); err == nil || code == "" {
			t.Errorf("%s: err = %v, code %q", name, err, code)
		}
	}
	if len(m.catalog.List()) != 0 {
		t.Errorf("refused files reached the catalog: %v", m.catalog.List())
	}
}

// An upload is moved into place, not copied again.
func TestAdoptUpload(t *testing.T) {
	m, storage, dir := importManager(t)
	up, err := m.UploadPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(up, gguftest.Bytes(gguftest.Options{Name: "Uploaded"}), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := m.AdoptUpload(context.Background(), ImportRequest{Path: "phone.gguf"}, up)
	if err != nil {
		t.Fatal(err)
	}
	if path := waitInstalled(t, storage, got.ID); path != storage.ModelPath(got.ID) {
		t.Errorf("installed at %s", path)
	}
	if _, err := os.Stat(up); !os.IsNotExist(err) {
		t.Error("the upload is still in the partial folder")
	}
	if _, err := m.AdoptUpload(context.Background(), ImportRequest{}, filepath.Join(dir, "x.gguf")); err == nil {
		t.Error("adopted a file outside the models folder")
	}
}

// A model added from a file can be renamed and tagged, and given the
// vision projector found beside it, so it sees pictures (#467).
func TestAddedModelDetails(t *testing.T) {
	m, storage, dir := importManager(t)
	ctx := context.Background()
	folder := filepath.Join(dir, "lmstudio", "gemma")
	src := gguftest.Write(t, folder, "gemma-Q4_K_M.gguf", gguftest.Options{Name: "Gemma"})
	proj := gguftest.Write(t, folder, "mmproj-gemma-F16.gguf", gguftest.Options{Architecture: "clip", Type: "mmproj"})
	got, err := m.ImportFile(ctx, ImportRequest{Path: src, InPlace: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Projector != proj {
		t.Fatalf("projector offered = %q, want %q", got.Projector, proj)
	}
	name := "My Gemma"
	entry, err := m.UpdateAdded(ctx, got.ID, &name, []string{"Coding", "coding", "vision", "nonsense"})
	if err != nil {
		t.Fatal(err)
	}
	if entry.DisplayName != "My Gemma" || len(entry.Tags) != 1 || entry.Tags[0] != "coding" {
		t.Errorf("entry = %+v", entry)
	}

	// A model isn't a projector, and a projector needs an installed model.
	if err := m.SetProjector(ctx, got.ID, src); err == nil {
		t.Error("a model was taken as a projector")
	}
	if err := m.SetProjector(ctx, "nope", proj); err == nil {
		t.Error("a projector was set for a model that isn't here")
	}
	if err := m.SetProjector(ctx, got.ID, proj); err != nil {
		t.Fatal(err)
	}
	if !m.SeesImages(got.ID) || m.ProjectorPath(got.ID) != storage.ProjectorPath(got.ID) {
		t.Errorf("sees images = %v, projector at %q", m.SeesImages(got.ID), m.ProjectorPath(got.ID))
	}
	if e, _ := m.catalog.Get(got.ID); !contains(e.Tags, "vision") || !contains(e.Tags, "coding") {
		t.Errorf("tags after the projector = %v", e.Tags)
	}
	// Retagging keeps vision, which comes with the projector.
	if e, _ := m.UpdateAdded(ctx, got.ID, nil, []string{"general"}); !contains(e.Tags, "vision") || !contains(e.Tags, "general") {
		t.Errorf("tags after retagging = %v", e.Tags)
	}

	// A catalog model keeps its own name.
	m.catalog.Upsert(CatalogEntry{ID: "catalog-model", DisplayName: "Catalog"})
	if _, err := m.UpdateAdded(ctx, "catalog-model", &name, nil); err == nil {
		t.Error("renamed a catalog model")
	}
}

// A copy of a catalog model, named as Toskar saves it or as its source
// names it, is that model: installed under its ID, not a new one, so it
// isn't offered for download again.
func TestImportRecognizesCatalogModels(t *testing.T) {
	m, storage, dir := importManager(t)
	ctx := context.Background()
	write := func(sub, name, arch string) string {
		return gguftest.Write(t, filepath.Join(dir, sub), name, gguftest.Options{Name: "Tiny Llama", FileType: 15, Architecture: arch})
	}
	probe := write("probe", "probe.gguf", "llama")
	st, _ := os.Stat(probe)
	m.catalog.Upsert(CatalogEntry{
		ID: "tiny-llama-q4", DisplayName: "Tiny Llama (catalog)", SizeBytes: uint64(st.Size()),
		Source:   contracts.ModelSource{URL: "https://huggingface.co/x/y/resolve/main/Tiny-Llama-Q4_K_M.gguf", Format: "gguf"},
		Training: &TrainingInfo{Architecture: "llama"},
	})

	// Another Toskar's download, used in place.
	other := write("other-toskar", "tiny-llama-q4.gguf", "llama")
	got, err := m.ImportFile(ctx, ImportRequest{Path: other, InPlace: true})
	if err != nil || got.ID != "tiny-llama-q4" {
		t.Fatalf("Toskar's own copy: %+v, %v", got, err)
	}
	if waitInstalled(t, storage, "tiny-llama-q4") != other {
		t.Fatal("not installed in place")
	}
	if entry, _ := m.catalog.Get("tiny-llama-q4"); entry.DisplayName != "Tiny Llama (catalog)" {
		t.Fatalf("catalog entry replaced: %+v", entry)
	}
	// The same file again keeps the catalog ID.
	if again, _ := m.ImportFile(ctx, ImportRequest{Path: other, InPlace: true}); again.ID != "tiny-llama-q4" {
		t.Fatalf("again: %s", again.ID)
	}
	// A second copy, named as its source names it, is its own model now.
	lm := write("lmstudio", "Tiny-Llama-Q4_K_M.gguf", "llama")
	if second, _ := m.ImportFile(ctx, ImportRequest{Path: lm, InPlace: true}); second.ID == "tiny-llama-q4" {
		t.Fatal("a second copy took the installed catalog ID")
	}
	// Named like it, but another architecture, isn't it.
	if err := m.Delete(ctx, "tiny-llama-q4"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("deleting the model deleted the other Toskar's file: %v", err)
	}
	wrong := write("wrong", "tiny-llama-q4.gguf", "qwen2")
	if notIt, _ := m.ImportFile(ctx, ImportRequest{Path: wrong, InPlace: true}); notIt.ID == "tiny-llama-q4" {
		t.Fatal("another architecture matched")
	}
}
