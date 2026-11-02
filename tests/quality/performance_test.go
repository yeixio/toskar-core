package quality

import (
	"encoding/json"
	"math"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/benchmark"
	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// performance is how a model ran on this computer, in the terms people running
// models compare: reading a long prompt apart from writing the answer, the wait
// from sending to the last word, the model's exact file and window, and where it
// ran. It's beside a row's Score and never in it (#1037).
type performance struct {
	Model string `json:"model"`
	// PromptTokens is the prompt these were measured with.
	PromptTokens          int     `json:"prompt_tokens"`
	PromptTokensPerSecond float64 `json:"prompt_tokens_per_second"`
	TokensPerSecond       float64 `json:"tokens_per_second"`
	FirstTokenMs          float64 `json:"first_token_ms"`
	// EffectiveTokensPerSecond counts from sending to the last token, wait included.
	EffectiveTokensPerSecond float64 `json:"effective_tokens_per_second"`
	GPULayers                int     `json:"gpu_layers"`
	GPULayersTotal           int     `json:"gpu_layers_total"`
	GPUMemoryBytes           uint64  `json:"gpu_memory_bytes,omitempty"`
	MemoryBytes              uint64  `json:"memory_bytes,omitempty"`
	ContextWindow            int     `json:"context_window,omitempty"`
	Quantization             string  `json:"quantization,omitempty"`
}

// TestPerformance measures TOSKAR_QUALITY_MODEL with the long prompt benchmark,
// and writes TOSKAR_QUALITY_PERFORMANCE_REPORT.
func TestPerformance(t *testing.T) {
	path, url := config.Env("QUALITY_PERFORMANCE_REPORT"), config.Env("QUALITY_URL")
	model := config.Env("QUALITY_MODEL")
	if path == "" || url == "" || model == "" || model == "recommended" {
		t.Skip("needs TOSKAR_QUALITY_PERFORMANCE_REPORT, TOSKAR_QUALITY_URL, and one TOSKAR_QUALITY_MODEL")
	}
	d := newRealDriver(strings.TrimRight(url, "/"))
	d.model = model
	d.installModel(t, model)

	var job contracts.BenchmarkJob
	req := contracts.BenchmarkRequest{ModelIDs: []string{model}, WorkloadIDs: []string{benchmark.LongPromptWorkload}, Runs: 3}
	if status := d.do(t, http.MethodPost, "/api/v1/benchmarks", req, &job); status >= 300 {
		t.Fatalf("starting the benchmark: status %d", status)
	}
	for deadline := time.Now().Add(60 * time.Minute); ; time.Sleep(2 * time.Second) {
		if time.Now().After(deadline) {
			t.Fatalf("the benchmark didn't finish: %+v", job.Progress)
		}
		d.do(t, http.MethodGet, "/api/v1/benchmarks/"+job.ID, nil, &job)
		if job.Status != contracts.BenchmarkPending && job.Status != contracts.BenchmarkRunning {
			break
		}
	}
	if job.Status != contracts.BenchmarkCompleted || len(job.Summaries) == 0 {
		t.Fatalf("the benchmark ended %s: %s", job.Status, job.Error)
	}
	summary := job.Summaries[0]
	if summary.ModelID != model {
		t.Fatalf("the benchmark measured %s and not %s", summary.ModelID, model)
	}
	got := performance{
		Model:                 model,
		PromptTokensPerSecond: round1(summary.AvgPromptTokPerSec),
		TokensPerSecond:       round1(summary.AvgEvalTokPerSec),
		FirstTokenMs:          math.Round(summary.AvgTTFTMs),
	}
	completion, total := 0, 0.0
	for _, s := range job.Samples {
		if s.Warmup {
			continue
		}
		if s.Error != "" {
			t.Fatalf("a run failed: %s", s.Error)
		}
		got.PromptTokens = s.PromptTokens
		completion += s.CompletionTokens
		total += s.TotalMs
	}
	if total > 0 {
		got.EffectiveTokensPerSecond = round1(float64(completion) / (total / 1000))
	}
	// A figure that wasn't measured doesn't get published as zero.
	if got.TokensPerSecond <= 0 || got.PromptTokensPerSecond <= 0 || got.FirstTokenMs <= 0 || got.EffectiveTokensPerSecond <= 0 {
		t.Fatalf("the benchmark didn't measure it all: %+v", got)
	}

	var running []contracts.RunningModelView
	d.do(t, http.MethodGet, "/api/v1/models/running", nil, &running)
	for _, r := range running {
		if r.ModelID != model || r.Mode != "" {
			continue
		}
		got.MemoryBytes = r.MemoryBytes
		if r.Acceleration != nil {
			got.GPULayers, got.GPULayersTotal = r.Acceleration.LayersOffloaded, r.Acceleration.LayersTotal
			got.GPUMemoryBytes = r.Acceleration.GPUMemoryBytes
		}
	}
	var models []contracts.Model
	d.do(t, http.MethodGet, "/api/v1/models", nil, &models)
	for _, m := range models {
		if m.ID == model {
			got.ContextWindow, got.Quantization = m.ContextWindow, m.Variant
		}
	}
	raw, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("%s", raw)
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
