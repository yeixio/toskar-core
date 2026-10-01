package llamacpp

import (
	"testing"
	"time"
)

func TestMergeServerMetricsFromTimings(t *testing.T) {
	started := time.Now().Add(-200 * time.Millisecond)
	first := started.Add(40 * time.Millisecond)
	ended := started.Add(180 * time.Millisecond)
	m := mergeServerMetrics(
		nil,
		&timingsPayload{
			PromptN:            36,
			PromptMS:           36.7,
			PromptPerSecond:    980,
			PredictedN:         12,
			PredictedMS:        70,
			PredictedPerSecond: 171.4,
		},
		started, first, ended, "hello there world",
	)
	if m.PromptTokens != 36 || m.CompletionTokens != 12 {
		t.Fatalf("tokens: %+v", m)
	}
	if m.EvalTokPerSec < 170 || m.EvalTokPerSec > 172 {
		t.Fatalf("eval tok/s: %v", m.EvalTokPerSec)
	}
	if m.PromptMs != 36.7 || m.EvalMs != 70 {
		t.Fatalf("ms: %+v", m)
	}
}

func TestPromptTokensIncludeCachedTokens(t *testing.T) {
	// A follow-up turn: llama-server reused 44 tokens and processed 1.
	started := time.Now().Add(-100 * time.Millisecond)
	m := mergeServerMetrics(nil, &timingsPayload{PromptN: 1, CacheN: 44, PromptMS: 5, PredictedN: 7},
		started, started.Add(10*time.Millisecond), time.Now(), "hi")
	if m.PromptTokens != 45 || m.CachedTokens != 44 {
		t.Fatalf("prompt %d cached %d", m.PromptTokens, m.CachedTokens)
	}
	// Speed is for the tokens actually processed.
	if m.PromptTokPerSec != 200 {
		t.Fatalf("prompt tok/s %v", m.PromptTokPerSec)
	}
}

func TestMergeServerMetricsFallbackEstimate(t *testing.T) {
	started := time.Now().Add(-100 * time.Millisecond)
	ended := time.Now()
	m := mergeServerMetrics(nil, nil, started, time.Time{}, ended, "one two three four")
	if m.CompletionTokens != 4 {
		t.Fatalf("expected estimated 4 tokens, got %d", m.CompletionTokens)
	}
	if m.TotalMs <= 0 {
		t.Fatal("expected total ms")
	}
}

func TestStreamDataError(t *testing.T) {
	msg, ok := streamDataError(`{"error":{"message":"Compute error.","type":"server_error","code":500}}`)
	if !ok || msg != "Compute error." {
		t.Fatalf("message %q ok=%v", msg, ok)
	}
	if _, ok := streamDataError(`{"choices":[{"delta":{"content":"Hi"}}]}`); ok {
		t.Fatal("a token frame is not an error")
	}
}

func TestContextWindowUsesTheRunningLimit(t *testing.T) {
	if ContextWindow(0) != 8192 || ContextWindow(131072) != 8192 || ContextWindow(4096) != 4096 {
		t.Fatalf("window(0)=%d window(128k)=%d window(4k)=%d", ContextWindow(0), ContextWindow(131072), ContextWindow(4096))
	}
}
