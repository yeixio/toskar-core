package app

import (
	"strings"
	"testing"
)

// What a picture or clip was cut back to is said under the answer, in the
// App language (#529).
func TestMediaLimitNotice(t *testing.T) {
	clip := map[string]any{"model": "Wan 2.2 TI2V 5B", "limited": map[string]any{
		"asked_seconds": 10.0, "made_seconds": 5.0, "max_seconds": 5.0,
		"asked_width": 1920, "asked_height": 1080, "made_width": 832, "made_height": 480}}
	got := mediaLimitNotice("en", "video.generate", clip)
	for _, want := range []string{"Clips can be up to 5 s long for now, so this one is 5 s instead of the 10 s asked for.", "Wan 2.2 TI2V 5B makes clips up to 832×480, so this one is smaller than the 1920×1080 asked for."} {
		if !strings.Contains(got, want) {
			t.Errorf("notice %q lacks %q", got, want)
		}
	}
	if ja := mediaLimitNotice("ja", "video.generate", clip); !strings.Contains(ja, "10 秒ではなく 5 秒") {
		t.Errorf("Japanese notice %q", ja)
	}
	picture := map[string]any{"model": "FLUX.2 [klein] 4B", "limited": map[string]any{"asked_width": 3840.0, "asked_height": 2160.0, "made_width": 1600.0, "made_height": 896.0}}
	if got := mediaLimitNotice("en", "image.generate", picture); got != "FLUX.2 [klein] 4B makes pictures up to 1600×896, so this one is smaller than the 3840×2160 asked for." {
		t.Errorf("picture notice %q", got)
	}
	if got := mediaLimitNotice("en", "image.generate", map[string]any{"width": 1024}); got != "" {
		t.Errorf("notice for a picture made as asked: %q", got)
	}
	missing := map[string]any{"model": "FLUX.2 [klein] 4B", "limited": map[string]any{"quality_model": "Z-Image-Turbo"}}
	if got := mediaLimitNotice("en", "image.generate", missing); got != "Z-Image-Turbo gives the quality asked for, but it isn't set up here, so this was made with FLUX.2 [klein] 4B. You can set it up in Settings › Tools." {
		t.Errorf("quality notice %q", got)
	}
}

// A profile's picture and clip quality fills in a request that asks for
// none, and only for those tools (#549).
func TestWithMediaQuality(t *testing.T) {
	args := map[string]any{"prompt": "a dog"}
	if got := withMediaQuality("best", "image.generate", args); got["quality"] != "best" || args["quality"] != nil {
		t.Fatalf("image = %v (original %v)", got, args)
	}
	if got := withMediaQuality("best", "video.generate", map[string]any{"quality": "fast"}); got["quality"] != "fast" {
		t.Fatalf("asked quality replaced: %v", got)
	}
	if got := withMediaQuality("best", "image.edit", args); got["quality"] != nil {
		t.Fatalf("edit got a quality: %v", got)
	}
	if got := withMediaQuality("", "image.generate", args); got["quality"] != nil {
		t.Fatalf("no profile quality: %v", got)
	}
}

// The steps say which model made a picture or clip (#549).
func TestMediaStepNamesTheModel(t *testing.T) {
	tr := &turnTrace{lang: "en"}
	tr.tool("image.generate", map[string]any{"prompt": "a dog"}, map[string]any{"model": "Z-Image-Turbo"})
	tr.tool("video.generate", map[string]any{"prompt": "waves"}, map[string]any{"model": "Wan 2.2 TI2V 5B"})
	var texts []string
	for _, s := range tr.steps {
		texts = append(texts, s.Text)
	}
	got := strings.Join(texts, " | ")
	if !strings.Contains(got, "Made the picture with Z-Image-Turbo") || !strings.Contains(got, "Made the clip with Wan 2.2 TI2V 5B") {
		t.Fatalf("steps %q", got)
	}
}

// A made picture's file carries how it was made, and has one step, naming
// its model (#553, #549).
func TestMadePictureCarriesDetails(t *testing.T) {
	tr := &turnTrace{lang: "en"}
	tr.tool("image.generate", map[string]any{"prompt": "a dog"}, map[string]any{
		"id": "a1", "name": "dog.png", "kind": "image", "model": "Z-Image-Turbo", "seed": int64(7), "prompt": "a dog", "width": 1024, "height": 576, "seconds": 35.0,
	})
	if len(tr.files) != 1 || tr.files[0].Details["model"] != "Z-Image-Turbo" || tr.files[0].Details["seed"] != int64(7) || tr.files[0].Details["seconds"] != nil {
		t.Fatalf("files %+v", tr.files)
	}
	if len(tr.steps) != 1 || tr.steps[0].Text != "Made the picture with Z-Image-Turbo" {
		t.Fatalf("steps %+v", tr.steps)
	}
}
