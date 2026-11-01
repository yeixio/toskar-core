package llamacpp

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// A chat model speculates when llama-server can; supporting models, a
// model with a projector, and TOSKAR_SPECULATIVE=off don't (#835).
func TestSpeculativeArgs(t *testing.T) {
	chat := pluginapi.ModelStartConfig{ModelPath: "m.gguf"}
	if got := speculativeArgs(chat, true); !slices.Equal(got, []string{"--spec-type", "ngram-mod"}) {
		t.Errorf("chat: %v", got)
	}
	for name, c := range map[string]struct {
		cfg       pluginapi.ModelStartConfig
		supported bool
	}{
		"unsupported": {chat, false},
		"embedding":   {pluginapi.ModelStartConfig{ModelPath: "e.gguf", Mode: pluginapi.ModeEmbedding}, true},
		"reranking":   {pluginapi.ModelStartConfig{ModelPath: "r.gguf", Mode: pluginapi.ModeReranking}, true},
		"projector":   {pluginapi.ModelStartConfig{ModelPath: "v.gguf", Projector: "v.mmproj.gguf"}, true},
		"turned off":  {pluginapi.ModelStartConfig{ModelPath: "m.gguf", NoSpeculation: true}, true},
	} {
		if got := speculativeArgs(c.cfg, c.supported); got != nil {
			t.Errorf("%s: %v", name, got)
		}
	}
	t.Setenv("TOSKAR_SPECULATIVE", "off")
	if got := speculativeArgs(chat, true); got != nil {
		t.Errorf("off: %v", got)
	}
}

// A model with a draft guesses with it and with the chat; turned off, it
// starts without the draft too.
func TestSpeculativeArgsWithDraft(t *testing.T) {
	cfg := pluginapi.ModelStartConfig{ModelPath: "m.gguf", Draft: "m.draft.gguf"}
	got := speculativeArgs(cfg, true)
	want := []string{"-md", "m.draft.gguf", "-ngld", "99", "--spec-type", "draft-simple,ngram-mod"}
	if !slices.Equal(got, want) || draftFor(cfg, true) != "m.draft.gguf" {
		t.Errorf("draft: %v", got)
	}
	cfg.NoSpeculation = true
	if got := speculativeArgs(cfg, true); got != nil || draftFor(cfg, true) != "" {
		t.Errorf("turned off: %v", got)
	}
	if draftFor(pluginapi.ModelStartConfig{ModelPath: "m.gguf", Draft: "d.gguf"}, false) != "" {
		t.Error("an old llama-server has no draft")
	}
}

// Support is read from the binary's help once, and again after an update.
func TestSupportsSpeculationAsksOncePerBinary(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "llama-server")
	if err := os.WriteFile(bin, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	help := "--spec-type none,draft-simple"
	asked := 0
	saved := runHelp
	runHelp = func(context.Context, string) ([]byte, error) { asked++; return []byte(help), nil }
	t.Cleanup(func() { runHelp = saved })

	ctx := context.Background()
	first, again := supportsSpeculation(ctx, bin), supportsSpeculation(ctx, bin)
	if first || again || asked != 1 {
		t.Fatalf("old build: asked %d", asked)
	}
	// An update replaces the binary.
	help = "--spec-type none,draft-simple,ngram-mod,ngram-cache"
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(bin, later, later); err != nil {
		t.Fatal(err)
	}
	if !supportsSpeculation(ctx, bin) || asked != 2 {
		t.Fatalf("new build: asked %d", asked)
	}
	if supportsSpeculation(ctx, filepath.Join(t.TempDir(), "missing")) {
		t.Fatal("a missing binary supports nothing")
	}
}

// llama-server's draft counts reach the metrics.
func TestDraftCountsInMetrics(t *testing.T) {
	now := time.Now()
	m := mergeServerMetrics(nil, &timingsPayload{PredictedN: 200, DraftN: 128, DraftNAccepted: 85}, now, now, now, "")
	if m.DraftTokens != 128 || m.DraftAccepted != 85 {
		t.Fatalf("draft %d accepted %d", m.DraftTokens, m.DraftAccepted)
	}
}

// A build with --fit places the layers itself (#955); a number asked for,
// or an older build, gets -ngl.
func TestGPULayerArgs(t *testing.T) {
	for _, c := range []struct {
		layers int
		fits   bool
		want   []string
	}{
		{0, true, nil},
		{0, false, []string{"-ngl", "99"}},
		{20, true, []string{"-ngl", "20"}},
		{20, false, []string{"-ngl", "20"}},
	} {
		if got := gpuLayerArgs(c.layers, c.fits); !reflect.DeepEqual(got, c.want) {
			t.Errorf("layers %d fits %v: %v, want %v", c.layers, c.fits, got, c.want)
		}
	}
}

// --fit is read from the binary's help, like speculation.
func TestSupportsFit(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "llama-server")
	if err := os.WriteFile(bin, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	saved := runHelp
	runHelp = func(context.Context, string) ([]byte, error) {
		return []byte("-fit,  --fit [on|off]\n-fitt, --fit-target MiB0,MiB1,MiB2,..."), nil
	}
	t.Cleanup(func() { runHelp = saved })
	if !supportsFit(context.Background(), bin) {
		t.Error("a build with --fit-target fits")
	}
	if supportsFit(context.Background(), filepath.Join(t.TempDir(), "missing")) {
		t.Error("a missing binary fits nothing")
	}
}
