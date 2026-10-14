package imagegen

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A model not yet tested on real hardware is offered but never recommended,
// and neither is one that only makes or only edits (#548).
func TestUntestedIsNeverRecommended(t *testing.T) {
	for _, mem := range []int64{8 << 30, 16 << 30, 64 << 30} {
		if m := Recommend(mem); m.Untested {
			t.Errorf("%d GB recommends untested %s", mem>>30, m.ID)
		}
	}
	z, ok := Lookup("z-image-turbo")
	if !ok || z.Untested || z.Edits || z.License != "Apache-2.0" {
		t.Fatalf("z-image-turbo = %+v", z)
	}
	// Tested now, but it doesn't edit, so it's chosen, never recommended.
	for _, mem := range []int64{8 << 30, 16 << 30, 64 << 30} {
		if m := Recommend(mem); !m.Edits || m.EditOnly {
			t.Errorf("%d GB recommends %s, which doesn't do everything", mem>>30, m.ID)
		}
		if m := recommendIn(VideoCatalog(), mem); !m.Edits {
			t.Errorf("%d GB recommends clip model %s, which takes no picture", mem>>30, m.ID)
		}
	}
}

// With a model that can't edit set up as the active one, an edit uses an
// installed model that can; with none, editing is unavailable and says
// what to set up (#548).
func TestEditsUseAModelThatEdits(t *testing.T) {
	f := newFixture(t)
	base := f.model(t)
	editor, photo := base, base
	editor.ID, editor.Name, editor.Edits = "editor", "Editor", true
	photo.ID, photo.Name, photo.Edits = "photo", "Photo", false
	editor.Files = append([]File(nil), base.Files...)
	photo.Files = append([]File(nil), base.Files...)
	f.setup.Catalog = func() []Model { return []Model{editor, photo} }
	eng := &Engine{Setup: f.setup, WorkDir: t.TempDir()}
	edits := &EditTool{Engine: eng}
	ctx := context.Background()
	archive, _ := f.setup.platform()

	if err := f.setup.install(ctx, &job{}, photo, true, archive); err != nil {
		t.Fatal(err)
	}
	if ok, why := edits.Available(); ok || !strings.Contains(why, "Photo can't edit pictures. Set up Editor") {
		t.Fatalf("edit available = %v, %q", ok, why)
	}
	if _, err := eng.Generate(ctx, Request{Prompt: "make it blue", Reference: samplePNG(t, 64, 64), RefName: "a.png"}); err == nil || !strings.Contains(err.Error(), "can't edit pictures") {
		t.Fatalf("edit without an editor: %v", err)
	}

	if err := f.setup.install(ctx, &job{}, editor, false, archive); err != nil {
		t.Fatal(err)
	}
	// The picture model stays the active one.
	if err := os.WriteFile(filepath.Join(f.setup.ModelsDir, "active"), []byte("photo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, why := edits.Available(); !ok {
		t.Fatalf("edit unavailable: %s", why)
	}
	if _, err := eng.Generate(ctx, Request{Prompt: "make it blue", Reference: samplePNG(t, 64, 64), RefName: "a.png"}); err != nil {
		t.Fatal(err)
	}
	if got := flag(args(t, f), "--diffusion-model"); !strings.Contains(got, string(filepath.Separator)+"editor"+string(filepath.Separator)) {
		t.Fatalf("edit used %s", got)
	}
	if _, err := eng.Generate(ctx, Request{Prompt: "a dog"}); err != nil {
		t.Fatal(err)
	}
	if got := flag(args(t, f), "--diffusion-model"); !strings.Contains(got, string(filepath.Separator)+"photo"+string(filepath.Separator)) {
		t.Fatalf("a new picture used %s", got)
	}
}

// A model that only edits is preferred for edits, never used for new
// pictures, never made the active model, and gets its own arguments: its
// text encoder's image encoder, flow shift, and model args (#548).
func TestEditOnlyModel(t *testing.T) {
	q, ok := Lookup("qwen-image-edit-2511")
	if !ok || !q.EditOnly || !q.Edits || q.Untested || q.ModelArgs != "qwen_image_zero_cond_t=true" {
		t.Fatalf("qwen-image-edit-2511 = %+v", q)
	}
	f := newFixture(t)
	base := f.model(t)
	picture, editor := base, base
	picture.ID, picture.Name, picture.Edits = "picture", "Picture", true
	editor.ID, editor.Name, editor.Edits, editor.EditOnly = "editor", "Editor", true, true
	editor.FlowShift, editor.ModelArgs = 3, "zero_cond_t=true"
	f.files["vision.gguf"] = []byte("vision")
	editor.Files = append(append([]File(nil), base.Files...), File{Role: "llm_vision", Path: "vision.gguf", Size: 6, SHA256: sum([]byte("vision"))})
	f.setup.Catalog = func() []Model { return []Model{picture, editor} }
	eng := &Engine{Setup: f.setup, WorkDir: t.TempDir()}
	ctx := context.Background()
	archive, _ := f.setup.platform()
	if err := f.setup.install(ctx, &job{}, picture, true, archive); err != nil {
		t.Fatal(err)
	}
	if err := f.setup.install(ctx, &job{}, editor, false, archive); err != nil {
		t.Fatal(err)
	}
	if got := f.setup.ActiveModel(); got != "picture" {
		t.Fatalf("active = %s", got)
	}
	if _, err := eng.Generate(ctx, Request{Prompt: "make it blue", Reference: samplePNG(t, 64, 64), RefName: "a.png"}); err != nil {
		t.Fatal(err)
	}
	got := args(t, f)
	if !strings.Contains(flag(got, "--diffusion-model"), string(filepath.Separator)+"editor"+string(filepath.Separator)) ||
		filepath.Base(flag(got, "--llm_vision")) != "vision.gguf" || flag(got, "--flow-shift") != "3" || flag(got, "--model-args") != "zero_cond_t=true" {
		t.Fatalf("edit args %q", got)
	}
	if _, err := eng.Generate(ctx, Request{Prompt: "a dog"}); err != nil {
		t.Fatal(err)
	}
	got = args(t, f)
	if !strings.Contains(flag(got, "--diffusion-model"), string(filepath.Separator)+"picture"+string(filepath.Separator)) ||
		flag(got, "--llm_vision") != "" || flag(got, "--model-args") != "" {
		t.Fatalf("new picture args %q", got)
	}
	// With only the editor installed, nothing is active, so new pictures
	// aren't made with it.
	os.RemoveAll(f.setup.modelDir("picture"))
	if got := f.setup.ActiveModel(); got != "" {
		t.Fatalf("the editor became active: %s", got)
	}
}

// The light clip model is untested and takes no picture; on a computer
// where no model that takes one fits, the message says so instead of
// naming one that can't be set up (#548).
func TestLightClipModel(t *testing.T) {
	m, ok := lookupIn(VideoCatalog(), "wan2.1-t2v-1.3b")
	if !ok || m.Untested || m.Edits || m.MemoryBytes >= 16<<30 {
		t.Fatalf("wan2.1-t2v-1.3b = %+v", m)
	}
	for _, v := range VideoCatalog() {
		for _, file := range v.Files {
			if file.Source == "" && file.Revision == "" || len(file.SHA256) != 64 || file.Size == 0 {
				t.Errorf("%s: %s is not pinned", v.ID, file.Path)
			}
		}
	}
	small := &Setup{ModelsDir: t.TempDir(), Catalog: VideoCatalog, Memory: func() int64 { return 8 << 30 }}
	if got := editModelMissing(small); !strings.Contains(got, "no model that can fits this computer") {
		t.Fatalf("8 GB: %q", got)
	}
	big := &Setup{ModelsDir: t.TempDir(), Catalog: VideoCatalog, Memory: func() int64 { return 64 << 30 }}
	if got := editModelMissing(big); !strings.Contains(got, "Set up Wan 2.2 TI2V 5B") {
		t.Fatalf("64 GB: %q", got)
	}
}

// Every picture and clip model has a tier, and a request for a quality uses
// the installed model of that tier; when it isn't set up, the active model
// makes it and the result names the one that would (#549).
func TestQualityTiers(t *testing.T) {
	for _, list := range [][]Model{Catalog(), VideoCatalog()} {
		tiers := map[string]bool{}
		for _, m := range list {
			// Edit-only and licensed models are chosen, not picked by tier.
			if !m.EditOnly && !m.NeedsLicense && m.Tier == "" {
				t.Errorf("%s has no tier", m.ID)
			}
			tiers[m.Tier] = true
		}
		for _, want := range []string{"fast", "balanced", "best"} {
			if !tiers[want] {
				t.Errorf("no %s model in %s's catalog", want, list[0].Kind)
			}
		}
	}
	for in, want := range map[string]string{"best": "best", "High": "best", "draft": "fast", "normal": "balanced", "": "", "purple": ""} {
		if got := qualityArg(in); got != want {
			t.Errorf("qualityArg(%q) = %q, want %q", in, got, want)
		}
	}

	f := newFixture(t)
	base := f.model(t)
	fast, best := base, base
	fast.ID, fast.Name, fast.Tier, fast.Edits = "fast", "Fast", "fast", true
	best.ID, best.Name, best.Tier, best.Edits = "best", "Best", "best", false
	balanced := base
	balanced.ID, balanced.Name, balanced.Tier = "balanced", "Balanced", "balanced"
	f.setup.Catalog = func() []Model { return []Model{fast, balanced, best} }
	eng := &Engine{Setup: f.setup, WorkDir: t.TempDir()}
	ctx := context.Background()
	archive, _ := f.setup.platform()
	if err := f.setup.install(ctx, &job{}, best, true, archive); err != nil {
		t.Fatal(err)
	}
	if err := f.setup.install(ctx, &job{}, fast, false, archive); err != nil {
		t.Fatal(err)
	}
	// Fast is active; best quality uses the best model.
	res, err := eng.Generate(ctx, Request{Prompt: "a dog", Quality: "best"})
	if err != nil || res.Model != "Best" || res.Tier != "best" || res.Missing != "" {
		t.Fatalf("best = %+v, %v", res, err)
	}
	// Balanced isn't set up: the active model makes it, and says which would.
	res, err = eng.Generate(ctx, Request{Prompt: "a dog", Quality: "balanced"})
	if err != nil || res.Model != "Fast" || res.Missing != "Balanced" {
		t.Fatalf("balanced = %+v, %v", res, err)
	}
	// No quality: the active model.
	if res, _ := eng.Generate(ctx, Request{Prompt: "a dog"}); res.Model != "Fast" || res.Missing != "" {
		t.Fatalf("default = %+v", res)
	}
	// An edit keeps its own routing whatever the quality.
	res, err = eng.Generate(ctx, Request{Prompt: "make it blue", Quality: "best", Reference: samplePNG(t, 64, 64), RefName: "a.png"})
	if err != nil || res.Model != "Fast" || res.Missing != "" {
		t.Fatalf("edit = %+v, %v", res, err)
	}
	// The tool reports the tier, and the missing model under limited.
	out := output(Result{PNG: []byte("x"), Width: 1, Height: 1, Model: "Fast", Tier: "fast", Missing: "Balanced"}, "", "a dog", 0, 0)
	if out.Result["quality"] != "fast" || out.Result["limited"].(map[string]any)["quality_model"] != "Balanced" {
		t.Fatalf("result %v", out.Result)
	}
}

// A model gets a verdict for this computer: unsupported when it can't
// fit, tight when it's below the memory or graphics memory it's
// comfortable with, and works otherwise; a tight one is set up only when
// the person agrees (#550).
func TestVerdicts(t *testing.T) {
	m := Model{Name: "Big", MemoryBytes: 16 << 30, GraphicsBytes: 12 << 30, Files: []File{{Size: 6 << 30}}}
	for _, c := range []struct {
		memory, graphics int64
		want             string
	}{
		{4 << 30, 0, VerdictUnsupported},
		{12 << 30, 0, VerdictTight},
		{32 << 30, 8 << 30, VerdictTight},
		{32 << 30, 24 << 30, VerdictWorks},
		{32 << 30, 0, VerdictWorks},
	} {
		if got := verdict(m, c.memory, c.graphics); got != c.want {
			t.Errorf("%d GB / %d GB graphics = %s, want %s", c.memory>>30, c.graphics>>30, got, c.want)
		}
	}
	// What it's short of, needs, and has, for "needs 12 GB; this computer
	// has 8 GB".
	for _, c := range []struct {
		memory, graphics int64
		shared           bool
		short            string
		need, have       int64
	}{
		{4 << 30, 0, false, ShortMemory, 8 << 30, 4 << 30},
		{12 << 30, 0, false, ShortMemory, 16 << 30, 12 << 30},
		{32 << 30, 8 << 30, false, ShortGraphics, 12 << 30, 8 << 30},
		{24 << 30, 10 << 30, true, ShortShared, 12 << 30, 10 << 30},
		{32 << 30, 24 << 30, false, "", 0, 0},
	} {
		short, need, have := shortOf(m, c.memory, c.graphics, c.shared)
		if short != c.short || need != c.need || have != c.have {
			t.Errorf("%d GB / %d GB graphics: %s %d %d, want %s %d %d", c.memory>>30, c.graphics>>30, short, need>>30, have>>30, c.short, c.need>>30, c.have>>30)
		}
	}
	if why := tightReason(m, 24<<30, 10<<30, true); !strings.Contains(why, "needs about 12 GB of graphics memory") || !strings.Contains(why, "can use 10 GB of its 24 GB") {
		t.Errorf("shared reason: %s", why)
	}
	for _, list := range [][]Model{Catalog(), VideoCatalog()} {
		for _, x := range list {
			if x.GraphicsBytes == 0 {
				t.Errorf("%s has no graphics memory figure", x.ID)
			}
		}
	}

	f := newFixture(t)
	model := f.model(t)
	model.GraphicsBytes = 64 << 30
	f.setup.Catalog = func() []Model { return []Model{model} }
	f.setup.Graphics = func() int64 { return 8 << 30 }
	st := f.setup.Status()
	if st.Models[0].Verdict != VerdictTight || !st.Models[0].GraphicsShort || st.Models[0].Short != ShortGraphics ||
		st.Models[0].NeedBytes != 64<<30 || st.Models[0].HaveBytes != 8<<30 || st.GraphicsBytes != 8<<30 {
		t.Fatalf("status %+v", st.Models[0])
	}
	if err := f.setup.Start(model.ID); err == nil || !strings.Contains(err.Error(), "graphics card has 8 GB") {
		t.Fatalf("set up without agreeing: %v", err)
	}
	f.setup.AllowTight(model.ID)
	if err := f.setup.Start(model.ID); err != nil {
		t.Fatal(err)
	}
	if j := waitJob(t, f.setup); j.Error != "" {
		t.Fatalf("setup failed: %s", j.Error)
	}
}
