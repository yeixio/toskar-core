package benchmark_test

import (
	"context"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/benchmark"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

func TestWorkloadsCatalog(t *testing.T) {
	ws := benchmark.Workloads()
	if len(ws) < 4 {
		t.Fatalf("expected multiple workloads, got %d", len(ws))
	}
	for _, w := range ws {
		if w.ID == "" || w.Name == "" || len(w.Prompts) == 0 {
			t.Fatalf("invalid workload: %+v", w)
		}
	}
}

func TestRunnerCompletesWithFakeChat(t *testing.T) {
	r := benchmark.NewRunner()
	r.ModelPath = func(ctx context.Context, modelID string) (string, error) {
		return "/tmp/" + modelID + ".gguf", nil
	}
	r.ListRunning = func(ctx context.Context) ([]pluginapi.RunningModel, error) {
		return nil, nil
	}
	r.StopModel = func(ctx context.Context, instanceID string) error { return nil }
	r.StartModel = func(ctx context.Context, modelID, modelPath string) (pluginapi.RunningModel, error) {
		return pluginapi.RunningModel{ID: "inst-" + modelID, ModelID: modelID, Endpoint: "http://127.0.0.1:9", Status: "running"}, nil
	}
	r.Chat = func(ctx context.Context, req pluginapi.ChatRequest) (<-chan pluginapi.ChatChunk, error) {
		ch := make(chan pluginapi.ChatChunk, 2)
		go func() {
			defer close(ch)
			ch <- pluginapi.ChatChunk{Content: "ok "}
			ch <- pluginapi.ChatChunk{Done: true, Metrics: &pluginapi.GenerationMetrics{
				PromptTokens: 20, CompletionTokens: 8, TTFTMs: 40, PromptMs: 30, EvalMs: 80, TotalMs: 120,
				PromptTokPerSec: 600, EvalTokPerSec: 100,
			}}
		}()
		return ch, nil
	}

	job, err := r.Start(context.Background(), contracts.BenchmarkRequest{
		ModelIDs:    []string{"model-a", "model-b"},
		WorkloadIDs: []string{"chat"},
		Runs:        1,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, err := r.Get(job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == contracts.BenchmarkCompleted {
			if len(got.Summaries) == 0 {
				t.Fatal("expected summaries")
			}
			if got.Winners["chat"] == "" {
				t.Fatal("expected chat winner")
			}
			return
		}
		if got.Status == contracts.BenchmarkFailed {
			t.Fatalf("failed: %s", got.Error)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timeout waiting for benchmark")
}

// Every sample says where the model ran, from the start's report (#317).
func TestSamplesKeepTheDevice(t *testing.T) {
	r := benchmark.NewRunner()
	r.ModelPath = func(ctx context.Context, modelID string) (string, error) { return "/tmp/" + modelID + ".gguf", nil }
	r.ListRunning = func(ctx context.Context) ([]pluginapi.RunningModel, error) { return nil, nil }
	r.StopModel = func(ctx context.Context, instanceID string) error { return nil }
	r.StartModel = func(ctx context.Context, modelID, modelPath string) (pluginapi.RunningModel, error) {
		return pluginapi.RunningModel{ID: "i", ModelID: modelID, Endpoint: "http://127.0.0.1:9", Status: "running",
			Acceleration: &pluginapi.Acceleration{Backend: "metal", Devices: []string{"Apple M2 Pro"}, LayersOffloaded: 29, LayersTotal: 29}}, nil
	}
	r.Chat = func(ctx context.Context, req pluginapi.ChatRequest) (<-chan pluginapi.ChatChunk, error) {
		ch := make(chan pluginapi.ChatChunk, 1)
		ch <- pluginapi.ChatChunk{Done: true, Metrics: &pluginapi.GenerationMetrics{CompletionTokens: 8, EvalMs: 80, EvalTokPerSec: 100}}
		close(ch)
		return ch, nil
	}
	job, err := r.Start(context.Background(), contracts.BenchmarkRequest{ModelIDs: []string{"m"}, WorkloadIDs: []string{"chat"}, Runs: 1})
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		got, err := r.Get(job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != contracts.BenchmarkCompleted {
			continue
		}
		if len(got.Samples) == 0 {
			t.Fatal("no samples")
		}
		for _, s := range got.Samples {
			if s.Backend != "metal" || s.Device != "Apple M2 Pro" {
				t.Errorf("sample %+v", s)
			}
		}
		return
	}
	t.Fatal("timeout")
}

// A cached prompt starts in no time, so every run of this one has to differ from
// its first word.
func TestLongPromptIsNewEachRun(t *testing.T) {
	w, ok := benchmark.WorkloadByID(benchmark.LongPromptWorkload)
	if !ok || len(w.Prompts) != 1 {
		t.Fatalf("%+v", w)
	}
	// About 4,000 tokens, at a rough four characters a token.
	if n := len(w.Prompts[0].Text); n < 10000 || n > 16000 {
		t.Errorf("%d characters", n)
	}
	r := benchmark.NewRunner()
	r.ModelPath = func(context.Context, string) (string, error) { return "/tmp/m.gguf", nil }
	r.ListRunning = func(context.Context) ([]pluginapi.RunningModel, error) { return nil, nil }
	r.StopModel = func(context.Context, string) error { return nil }
	r.StartModel = func(_ context.Context, modelID, _ string) (pluginapi.RunningModel, error) {
		return pluginapi.RunningModel{ID: "inst", ModelID: modelID, Endpoint: "http://127.0.0.1:1"}, nil
	}
	starts := make(chan string, 8)
	r.Chat = func(_ context.Context, req pluginapi.ChatRequest) (<-chan pluginapi.ChatChunk, error) {
		starts <- req.Messages[0].Content[:60]
		ch := make(chan pluginapi.ChatChunk, 1)
		ch <- pluginapi.ChatChunk{Done: true, Metrics: &pluginapi.GenerationMetrics{EvalTokPerSec: 1}}
		close(ch)
		return ch, nil
	}
	job, err := r.Start(context.Background(), contracts.BenchmarkRequest{
		ModelIDs: []string{"m"}, WorkloadIDs: []string{benchmark.LongPromptWorkload}, Runs: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		got, err := r.Get(job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == contracts.BenchmarkCompleted {
			break
		}
		if got.Status == contracts.BenchmarkFailed || time.Now().After(deadline) {
			t.Fatalf("%s: %s", got.Status, got.Error)
		}
	}
	close(starts)
	seen := map[string]bool{}
	for start := range starts {
		seen[start] = true
	}
	// Three measured runs, and the warm-up before them.
	if len(seen) != 4 {
		t.Errorf("%d different starts in 4 runs", len(seen))
	}
}
