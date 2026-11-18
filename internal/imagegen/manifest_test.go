package imagegen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/taxonomy"
	rootmanifests "github.com/yeixio/toskar-core/manifests"
)

// The built-in media.json reads, with every file pinned, and lists the
// models setup offers (#551).
func TestBuiltInManifest(t *testing.T) {
	m, err := parseManifest(rootmanifests.MediaJSON)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, x := range append(m.Images, m.Video...) {
		ids = append(ids, x.ID)
	}
	got := strings.Join(ids, " ")
	for _, want := range []string{"flux2-klein-4b", "z-image-turbo", "qwen-image-edit-2511", "wan2.2-ti2v-5b", "wan2.2-t2v-a14b", "wan2.1-t2v-1.3b"} {
		if !strings.Contains(got, want) {
			t.Errorf("media.json lacks %s", want)
		}
	}
	// Settings that Model keeps out of its JSON come through.
	if a14b, ok := lookupIn(VideoCatalog(), "wan2.2-t2v-a14b"); !ok || a14b.HighNoiseSteps != 8 || a14b.FPS != 16 || a14b.Steps != 10 {
		t.Fatalf("a14b = %+v", a14b)
	}
	if q, ok := Lookup("qwen-image-edit-2511"); !ok || q.ModelArgs == "" || q.FlowShift != 3 {
		t.Fatalf("qwen = %+v", q)
	}
	if f := Catalog()[0].Files[0]; !strings.Contains(f.URL(), "/resolve/"+f.Revision+"/") || f.Revision == "" {
		t.Fatalf("url %s", f.URL())
	}
}

// A media.json in the data folder replaces the built-in one; one with a
// file that isn't pinned is refused and the built-in one stays.
func TestUseManifest(t *testing.T) {
	t.Cleanup(func() {
		manifestMu.Lock()
		loaded = nil
		manifestMu.Unlock()
	})
	dir := t.TempDir()
	var m mediaManifest
	if err := json.Unmarshal(rootmanifests.MediaJSON, &m); err != nil {
		t.Fatal(err)
	}
	m.Images = append(m.Images, m.Images[0])
	m.Images[len(m.Images)-1].ID, m.Images[len(m.Images)-1].Name = "extra", "Extra"
	raw, _ := json.Marshal(m)
	good := filepath.Join(dir, "good.json")
	if err := os.WriteFile(good, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UseManifest(good); err != nil {
		t.Fatal(err)
	}
	if _, ok := Lookup("extra"); !ok {
		t.Fatal("the data folder's model wasn't offered")
	}
	m.Images[len(m.Images)-1].Files[0].SHA256 = ""
	raw, _ = json.Marshal(m)
	bad := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(bad, raw, 0o644)
	if err := UseManifest(bad); err == nil || !strings.Contains(err.Error(), "not pinned") {
		t.Fatalf("an unpinned file was accepted: %v", err)
	}
	if _, ok := Lookup("extra"); !ok {
		t.Fatal("a refused manifest replaced the one in use")
	}
}

// Every picture and clip model says whether its license allows business
// use (#593); one that doesn't is refused.
func TestManifestNeedsCommercial(t *testing.T) {
	for _, m := range append(Catalog(), VideoCatalog()...) {
		if m.Commercial == "" {
			t.Errorf("%s doesn't say whether a business may use it", m.ID)
		}
	}
	if m, ok := Lookup("flux2-klein-9b"); !ok || m.Commercial != "no" {
		t.Errorf("FLUX.2 [klein] 9B is non-commercial: %+v", m.Commercial)
	}
	var m mediaManifest
	if err := json.Unmarshal(rootmanifests.MediaJSON, &m); err != nil {
		t.Fatal(err)
	}
	m.Images[0].Commercial = ""
	raw, _ := json.Marshal(m)
	if _, err := parseManifest(raw); err == nil || !strings.Contains(err.Error(), "business use") {
		t.Fatalf("a model without commercial was accepted: %v", err)
	}
}

// Every built-in picture and clip model says what it does (#593). One of
// your own may leave it out, but not get it wrong.
func TestManifestCategories(t *testing.T) {
	for _, m := range append(Catalog(), VideoCatalog()...) {
		if m.PrimaryCategory != "creative" || len(m.Tasks) == 0 || len(m.OutputModalities) == 0 {
			t.Errorf("%s: %+v", m.ID, m.Profile)
		}
	}
	if q, _ := Lookup("qwen-image-edit-2511"); strings.Contains(strings.Join(q.Tasks, " "), "image_generation") {
		t.Errorf("an editor that can't make pictures lists %v", q.Tasks)
	}
	var m mediaManifest
	if err := json.Unmarshal(rootmanifests.MediaJSON, &m); err != nil {
		t.Fatal(err)
	}
	m.Video[0].Tasks = []string{"image_generation", "decompilation"}
	raw, _ := json.Marshal(m)
	if _, err := parseManifest(raw); err == nil || !strings.Contains(err.Error(), "decompilation") {
		t.Fatalf("a wrong task was accepted: %v", err)
	}
	m.Video[0].Profile = taxonomy.Profile{}
	raw, _ = json.Marshal(m)
	if _, err := parseManifest(raw); err != nil {
		t.Fatalf("a model without categories was refused: %v", err)
	}
}
