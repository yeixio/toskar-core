package imagegen

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/webm"
)

// A longer video is made as a series of shots, each from the last frame of
// the one before, joined into one clip (#790).
func TestShotsTool(t *testing.T) {
	f, eng, st, ctx := videoFixture(t)
	sample, err := os.ReadFile("../webm/testdata/b.webm")
	if err != nil {
		t.Fatal(err)
	}
	one, err := webm.Parse(sample)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(eng.Setup.CLI()), "sample.webm"), sample, 0o600); err != nil {
		t.Fatal(err)
	}
	if eng.Estimate(60) != 0 {
		t.Fatal("an estimate before any clip was made")
	}
	var seen []Progress
	ctx = WithProgress(ctx, func(p Progress) { seen = append(seen, p) })
	tool := &ShotsTool{Engine: eng, Store: st}
	res, err := tool.Execute(ctx, map[string]any{
		"shots": []any{
			map[string]any{"prompt": "A woman walks onto the beach", "seconds": float64(1)},
			"She sets down a towel",
			map[string]any{"prompt": "She runs into the waves.", "seconds": float64(9)},
		},
		"style": "golden-hour light, red swimsuit",
		"seed":  float64(7),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Each shot after the first drops the frame it starts from.
	frames := 3*len(one.Frames) - 2
	if res["name"] != "a-woman-walks-onto-the.webm" || res["kind"] != "video" || res["frames"] != frames || res["shots_made"] != 3 {
		t.Fatalf("result %v", res)
	}
	made := res["shots"].([]map[string]any)
	if len(made) != 3 || made[0]["seed"] != int64(7) || made[2]["seed"] != int64(9) || made[1]["prompt"] != "She sets down a towel" {
		t.Fatalf("shots %v", made)
	}
	_, data, err := st.Read(ctx, res["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if c, err := webm.Parse(data); err != nil || len(c.Frames) != frames {
		t.Fatalf("joined video: %v", err)
	}
	// The last shot starts from a frame, with the style line, and at the most a clip can be.
	got := args(t, f)
	if filepath.Base(flag(got, "-i")) != "first.png" || flag(got, "-p") != "She runs into the waves. golden-hour light, red swimsuit" || flag(got, "--video-frames") != "81" {
		t.Fatalf("last shot's sd-cli args %q", got)
	}
	if len(seen) > 0 && (seen[len(seen)-1].Shot != 3 || seen[len(seen)-1].Shots != 3) {
		t.Fatalf("progress %+v", seen[len(seen)-1])
	}
	// Now that a clip was made here, a longer video has an estimate.
	if eng.Estimate(60) <= 0 {
		t.Fatal("no estimate after a clip")
	}

	// A shot that fails after others keeps the video so far.
	res, err = tool.Execute(ctx, map[string]any{"shots": []any{"Waves", "fail", "Sunset"}})
	if err != nil {
		t.Fatal(err)
	}
	if res["shots_made"] != 1 || !strings.Contains(res["stopped"].(string), "shot 2 failed") || !strings.Contains(res["note"].(string), "Only 1 of 3 shots") {
		t.Fatalf("stopped: %v", res)
	}
	// The first shot failing is the tool failing.
	if _, err := tool.Execute(ctx, map[string]any{"shots": []any{"fail", "Waves"}}); err == nil || !strings.Contains(err.Error(), "shot 1") {
		t.Fatalf("first shot failing: %v", err)
	}
	// Stopping ends it with no video.
	stop, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := tool.Execute(stop, map[string]any{"shots": []any{"Waves", "Sunset"}}); err == nil {
		t.Fatal("a stopped video was made")
	}
}

func TestShotsArg(t *testing.T) {
	for _, bad := range []any{nil, []any{}, []any{"ok", ""}, []any{map[string]any{"seconds": float64(3)}}, make([]any, MaxShots+1)} {
		if _, err := ShotsArg(bad); err == nil {
			t.Errorf("%v was taken", bad)
		}
	}
	shots, err := ShotsArg([]any{"a", map[string]any{"prompt": " b ", "seconds": float64(0.2)}})
	if err != nil || shots[0].Seconds != 5 || shots[1].Prompt != "b" || shots[1].Seconds != 1 || ShotsLength(shots) != 6 {
		t.Fatalf("%+v %v", shots, err)
	}
	if withStyle("Rain falls.", " film grain ") != "Rain falls. film grain" || withStyle("Rain", "") != "Rain" {
		t.Fatal("style line")
	}
	var none *Engine
	if none.Estimate(10) != time.Duration(0) {
		t.Fatal("estimate without an engine")
	}
}

func TestShotsFor(t *testing.T) {
	for _, c := range []struct {
		asked, made float64
		n           int
	}{{30, 30, 6}, {7, 7, 2}, {5.5, 6, 2}, {600, MaxShots * MaxClipSeconds, MaxShots}} {
		shots, made := ShotsFor("Rain", c.asked)
		parsed, err := ShotsArg(shots)
		if err != nil || len(parsed) != c.n || made != c.made || ShotsLength(parsed) != made {
			t.Errorf("ShotsFor(%v) = %d shots, %v s (%v)", c.asked, len(parsed), made, err)
		}
	}
}
