package app

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/yeixio/toskar-core/internal/contextusage"
	"github.com/yeixio/toskar-core/internal/models"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// A turn leaves its answer more room with a model that thinks first, unless
// thinking is off, and the room a request asked for with max_tokens (#840).
func TestReplyReserveByModel(t *testing.T) {
	t.Setenv("TOSKAR_STUB_INFERENCE", "1")
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	a, err := New(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })
	a.Models.Catalog().Upsert(models.CatalogEntry{ID: "thinker", DisplayName: "Thinker", Tags: []string{"reasoning", "general"}, Context: 8192})
	a.Models.Catalog().Upsert(models.CatalogEntry{ID: "plain", DisplayName: "Plain", Tags: []string{"general"}, Context: 8192})
	ctx := context.Background()
	for _, tc := range []struct {
		model string
		opts  pluginapi.GenerateOptions
		want  int
	}{
		{"thinker", pluginapi.GenerateOptions{}, contextusage.ReplyReserve(8192, 0, true)},
		{"thinker", pluginapi.GenerateOptions{NoThinking: true}, contextusage.ReplyReserve(8192, 0, false)},
		{"plain", pluginapi.GenerateOptions{}, contextusage.ReplyReserve(8192, 0, false)},
		{"plain", pluginapi.GenerateOptions{MaxTokens: 3000}, 3000},
	} {
		env := &chatExecEnv{app: a, ctx: ctx, modelOverride: tc.model}
		if got := env.ReplyReserve(pluginapi.WithAnswerOptions(ctx, tc.opts)); got != tc.want {
			t.Errorf("%s %+v: %d, want %d", tc.model, tc.opts, got, tc.want)
		}
	}
	if think, plain := contextusage.ReplyReserve(8192, 0, true), contextusage.ReplyReserve(8192, 0, false); think <= plain {
		t.Errorf("a thinking model's room %d isn't more than %d", think, plain)
	}
	if a.knownWindow(ctx, "ext:somewhere") != 0 || a.knownWindow(ctx, "plain") == 0 {
		t.Error("an unknown model's window is 0 and a catalog model's is known")
	}
}
