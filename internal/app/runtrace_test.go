package app

import (
	"testing"
	"time"

	"github.com/yeixio/yggdrasil-core/internal/orchestrator/builtin/simple"
	"github.com/yeixio/yggdrasil-core/internal/runlog"
	"github.com/yeixio/yggdrasil-core/pkg/pluginapi"
)

func TestRunTraceFromStreamAndEvents(t *testing.T) {
	c := runlog.New("r", "", "", "chat")
	in := make(chan pluginapi.ChatChunk, 3)
	in <- pluginapi.ChatChunk{Content: "Hi"}
	in <- pluginapi.ChatChunk{Content: "!", Done: true, Metrics: &pluginapi.GenerationMetrics{PromptTokens: 40, CompletionTokens: 2, CachedTokens: 30, EvalTokPerSec: 55}}
	close(in)
	var text string
	for chunk := range traceGeneration(c, in, "m", "assistant", "This Mac", time.Now().Add(-time.Second)) {
		text += chunk.Content
	}
	traceEvent(c, simple.EventPlanCreated, map[string]any{"steps": []string{"a", "b"}, "parallel": false})
	traceEvent(c, simple.EventLookup, map[string]any{"query": "x"})
	traceEvent(c, simple.EventEffort, map[string]any{"effort": "thorough"})
	traceEvent(c, simple.EventVerified, map[string]any{"issues": 1, "fixed": 1})
	// The stream is consumed; give the wrapper's goroutine a moment to record.
	time.Sleep(20 * time.Millisecond)
	r := c.Finish(runlog.StatusCompleted, "")
	if text != "Hi!" || len(r.Models) != 1 || r.Models[0].CachedTokens != 30 || r.Models[0].FirstTokenMs < 1000 || r.Models[0].TokPerSec != 55 {
		t.Fatalf("text=%q models=%+v", text, r.Models)
	}
	if r.Workers != 2 || r.Effort != "Thorough" || r.VerificationPasses != 1 || len(r.Strategy) != 2 {
		t.Fatalf("run = %+v", r)
	}
	if traceGeneration(nil, in, "m", "", "", time.Now()) != (<-chan pluginapi.ChatChunk)(in) {
		t.Fatal("no collector should pass the stream through")
	}
}
