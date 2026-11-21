package imagegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/webm"
)

// A clip in the chat is continued from its last frame, and the two parts
// become one longer clip (#155).
func TestExtendTool(t *testing.T) {
	f, eng, st, ctx := videoFixture(t)
	first, err := os.ReadFile("../webm/testdata/a.webm")
	if err != nil {
		t.Fatal(err)
	}
	next, err := os.ReadFile("../webm/testdata/b.webm")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(eng.Setup.CLI()), "sample.webm"), next, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Save(ctx, artifacts.Input{ConversationID: "c1", Name: "waves.webm", Producer: artifacts.ProducerAssistant, Data: first,
		Details: map[string]any{"prompt": "Waves rolling onto a beach"}}); err != nil {
		t.Fatal(err)
	}
	tool := &ExtendTool{Engine: eng, Store: st}
	// No clip named and no prompt: the newest clip goes on as it was.
	res, err := tool.Execute(ctx, map[string]any{"seconds": float64(1)})
	if err != nil {
		t.Fatal(err)
	}
	if res["name"] != "waves-longer.webm" || res["kind"] != "video" || res["frames"] != 17 || res["continued"] != "waves.webm" || res["prompt"] != "Waves rolling onto a beach" {
		t.Fatalf("result %v", res)
	}
	a, data, err := st.Read(ctx, res["id"].(string))
	if err != nil || a.MimeType != "video/webm" || a.Details["prompt"] != "Waves rolling onto a beach" {
		t.Fatalf("clip %+v %v", a, err)
	}
	if c, err := webm.Parse(data); err != nil || len(c.Frames) != 17 {
		t.Fatalf("joined clip: %v", err)
	}
	got := args(t, f)
	if filepath.Base(flag(got, "-i")) != "first.png" || flag(got, "-p") != "Waves rolling onto a beach" {
		t.Fatalf("sd-cli args %q", got)
	}

	// A clip by name, with what happens next.
	res, err = tool.Execute(ctx, map[string]any{"clip": "waves.webm", "prompt": "A surfer paddles out", "name": "surf"})
	if err != nil || res["name"] != "surf.webm" || flag(args(t, f), "-p") != "A surfer paddles out" {
		t.Fatalf("named: %v %v", res, err)
	}
	if _, err := tool.Execute(ctx, map[string]any{"clip": "missing.webm"}); err == nil || !strings.Contains(err.Error(), "no clip named") {
		t.Fatalf("a missing clip: %v", err)
	}
	// A clip whose last frame can't be read alone says so.
	inter, _ := os.ReadFile("../webm/testdata/inter.webm")
	if _, err := st.Save(ctx, artifacts.Input{ConversationID: "c1", Name: "phone.webm", Producer: artifacts.ProducerUser, Data: inter}); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(ctx, map[string]any{"clip": "phone.webm", "prompt": "more"}); err == nil || !strings.Contains(err.Error(), "can't be continued") {
		t.Fatalf("an inter-frame clip: %v", err)
	}
}
