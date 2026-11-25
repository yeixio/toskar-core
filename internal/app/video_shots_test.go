package app

import (
	"testing"

	"github.com/yeixio/toskar-core/internal/imagegen"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A longer video's card says how long it is, and how long it takes here
// only once this computer has made a clip (#790).
func TestShotsEstimate(t *testing.T) {
	a := &App{videoEngine: &imagegen.Engine{WorkDir: t.TempDir()}}
	got := a.shotsEstimate(map[string]any{"shots": []any{"Sunrise", map[string]any{"prompt": "Boats", "seconds": float64(3)}}})
	if got["video_seconds"] != float64(8) {
		t.Fatalf("%v", got)
	}
	if _, ok := got["estimate_minutes"]; ok {
		t.Fatalf("an estimate before any clip: %v", got)
	}
	if got := a.shotsEstimate(map[string]any{"shots": "nope"}); got != nil {
		t.Fatalf("bad shots: %v", got)
	}
	if got := (&App{}).shotsEstimate(map[string]any{"shots": []any{"Rain"}}); got["video_seconds"] != float64(5) || got["estimate_minutes"] != nil {
		t.Fatalf("no engine: %v", got)
	}
}

// A clip asked for longer than one clip is made as shots, so the video is
// the length asked for (#1382).
func TestAsLongerVideo(t *testing.T) {
	withShots := contracts.AIProfile{Tools: []contracts.ToolPolicy{{ToolID: "video.generate", Policy: "allow"}, {ToolID: "video.shots", Policy: "ask"}}}
	args := map[string]any{"prompt": "Waves on a beach", "seconds": float64(5), "quality": "fast", "image": "beach.png"}

	tool, got, limited := asLongerVideo(withShots, "Make a 30 second video of waves", "video.generate", args)
	if tool != "video.shots" || limited != nil {
		t.Fatalf("tool %q, limited %v", tool, limited)
	}
	shots, _ := got["shots"].([]any)
	if len(shots) != 6 || got["quality"] != "fast" || got["image"] != "beach.png" {
		t.Fatalf("%v", got)
	}
	if parsed, err := imagegen.ShotsArg(got["shots"]); err != nil || imagegen.ShotsLength(parsed) != 30 || parsed[0].Prompt != "Waves on a beach" {
		t.Fatalf("shots %v: %v", parsed, err)
	}

	// The model's length counts when the person named none; a remainder
	// is a shorter last shot.
	if _, got, _ := asLongerVideo(withShots, "Make a longer video of waves", "video.generate", map[string]any{"prompt": "Waves", "seconds": float64(12)}); func() bool {
		p, _ := imagegen.ShotsArg(got["shots"])
		return len(p) != 3 || imagegen.ShotsLength(p) != 12 || p[2].Seconds != 2
	}() {
		t.Fatalf("12 s: %v", got)
	}

	// Past two minutes is made at two minutes, and says so.
	tool, got, limited = asLongerVideo(withShots, "a 5 minute video of a parade", "video.generate", map[string]any{"prompt": "A parade"})
	if shots, _ := got["shots"].([]any); tool != "video.shots" || len(shots) != imagegen.MaxShots || limited["asked_seconds"] != float64(300) || limited["made_seconds"] != float64(120) {
		t.Fatalf("5 min: %q %v %v", tool, got, limited)
	}

	for name, c := range map[string]struct {
		profile contracts.AIProfile
		message string
		tool    string
		args    map[string]any
	}{
		"one clip's length": {withShots, "a 4 second clip of rain", "video.generate", map[string]any{"prompt": "Rain"}},
		"no length":         {withShots, "a clip of rain", "video.generate", map[string]any{"prompt": "Rain"}},
		"no shots tool":     {contracts.AIProfile{Tools: []contracts.ToolPolicy{{ToolID: "video.generate", Policy: "allow"}}}, "a 30 second clip of rain", "video.generate", map[string]any{"prompt": "Rain"}},
		"not a clip":        {withShots, "a 30 second clip of rain", "image.generate", map[string]any{"prompt": "Rain"}},
		"no prompt":         {withShots, "a 30 second clip", "video.generate", map[string]any{}},
	} {
		if tool, _, _ := asLongerVideo(c.profile, c.message, c.tool, c.args); tool != c.tool {
			t.Errorf("%s: became %q", name, tool)
		}
	}
}
