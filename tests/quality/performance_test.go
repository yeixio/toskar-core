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
	health "github.com/yeixio/toskar-core/internal/models/health"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// performance is how a model ran on this computer, in the terms people running
// models compare: reading a long prompt apart from writing the answer, the wait
// from sending to the last word, the model's exact file and window, and where it
// ran. It's beside a row's Score and never in it (#1037).
type performance struct {
	Model string `json:"model"`
	// Runs is full, partial, cpu, or no; Why says why it's short of full.
	Runs string `json:"runs"`
	Why  string `json:"why,omitempty"`
	// Hardware is here for a model that doesn't run, with no report to name it.
	Hardware string `json:"hardware,omitempty"`
	// OS is the system it ran on, as the daemon names it.
	OS string `json:"os,omitempty"`
	// PromptTokens is the prompt these were measured with.
	PromptTokens          int     `json:"prompt_tokens"`
	PromptTokensPerSecond float64 `json:"prompt_tokens_per_second"`
	TokensPerSecond       float64 `json:"tokens_per_second"`
	FirstTokenMs          float64 `json:"first_token_ms"`
	// EffectiveTokensPerSecond counts from sending to the last token, wait included.
	EffectiveTokensPerSecond float64 `json:"effective_tokens_per_second"`
	Backend                  string  `json:"backend,omitempty"`
	GPULayers                int     `json:"gpu_layers"`
	GPULayersTotal           int     `json:"gpu_layers_total"`
	GPUMemoryBytes           uint64  `json:"gpu_memory_bytes,omitempty"`
	ContextWindow            int     `json:"context_window,omitempty"`
	Quantization             string  `json:"quantization,omitempty"`
	// LlamaCpp is the llama.cpp release it ran on, such as b12361 (#1066).
	LlamaCpp string `json:"llama_cpp,omitempty"`
	// Speculation is on when a draft guessed ahead during the runs, off
	// when nothing was guessed; DraftAccepted is the share of the guesses
	// the model kept (#835, #1066).
	Speculation   string  `json:"speculation,omitempty"`
	DraftAccepted float64 `json:"draft_accepted,omitempty"`
	// Devices are the GPUs it ran on, every one when there are several.
	Devices []string `json:"devices,omitempty"`
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
	var inventory contracts.HardwareInventory
	d.do(t, http.MethodGet, "/api/v1/hardware", nil, &inventory)

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
	// Running out of memory is this model's result on this computer. Anything
	// else that stops it is a failed run, which says nothing about the model.
	if failure := benchmarkFailure(job); failure != "" && health.OutOfMemory(failure) {
		writePerformance(t, path, performance{Model: model, Runs: "no", Why: "out_of_memory", Hardware: d.hardware(t), OS: inventory.OS})
		return
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
		OS:                    inventory.OS,
		PromptTokensPerSecond: round1(summary.AvgPromptTokPerSec),
		TokensPerSecond:       round1(summary.AvgEvalTokPerSec),
		FirstTokenMs:          math.Round(summary.AvgTTFTMs),
	}
	completion, total := 0, 0.0
	drafted, accepted := 0, 0
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
		drafted += s.DraftTokens
		accepted += s.DraftAccepted
	}
	if total > 0 {
		got.EffectiveTokensPerSecond = round1(float64(completion) / (total / 1000))
	}
	got.Speculation, got.DraftAccepted = speculation(drafted, accepted)
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
		if r.Acceleration != nil {
			got.Runs, got.Why = runsFrom(r.Acceleration)
			got.GPULayers, got.GPULayersTotal = r.Acceleration.LayersOffloaded, r.Acceleration.LayersTotal
			got.GPUMemoryBytes, got.Backend = r.Acceleration.GPUMemoryBytes, r.Acceleration.Backend
			got.Devices = r.Acceleration.Devices
		}
	}
	var models []contracts.Model
	d.do(t, http.MethodGet, "/api/v1/models", nil, &models)
	for _, m := range models {
		if m.ID == model {
			got.ContextWindow, got.Quantization = m.ContextWindow, m.Variant
		}
	}
	got.LlamaCpp = llamaRelease(d, t)
	writePerformance(t, path, got)
}

// speculation is whether a draft guessed ahead during the runs, and the
// share of its guesses the model kept, as the runs counted them (#1066).
func speculation(drafted, accepted int) (string, float64) {
	if drafted <= 0 {
		return "off", 0
	}
	return "on", math.Round(float64(accepted)/float64(drafted)*100) / 100
}

// llamaRelease is the llama.cpp release the daemon runs, such as b12361,
// or "" when it doesn't know one (an install from before it wrote it down).
func llamaRelease(d realDriver, t *testing.T) string {
	var list []struct {
		ID        string `json:"id"`
		Detection struct {
			Version string `json:"version"`
		} `json:"detection"`
	}
	d.do(t, http.MethodGet, "/api/v1/runtimes", nil, &list)
	for _, r := range list {
		if v := r.Detection.Version; r.ID == "llamacpp" && strings.HasPrefix(v, "b") {
			return v
		}
	}
	return ""
}

// benchmarkFailure is why a benchmark or any of its runs failed, or "".
func benchmarkFailure(job contracts.BenchmarkJob) string {
	if job.Error != "" {
		return job.Error
	}
	for _, s := range job.Samples {
		if s.Error != "" {
			return s.Error
		}
	}
	return ""
}

// runsFrom says how a model ran from the runtime's own report.
func runsFrom(a *contracts.Acceleration) (string, string) {
	switch a.State {
	case contracts.AccelerationGPU:
		return "full", ""
	case contracts.AccelerationPartial:
		return "partial", a.Reason
	default:
		return "cpu", a.Reason
	}
}

func writePerformance(t *testing.T, path string, got performance) {
	t.Helper()
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

// Guessing ahead is on when a draft guessed during the runs, with the share
// of its guesses kept, and off when nothing was guessed (#1066).
func TestSpeculationFromTheRuns(t *testing.T) {
	for _, c := range []struct {
		drafted, accepted int
		want              string
		share             float64
	}{{0, 0, "off", 0}, {400, 288, "on", 0.72}, {3, 0, "on", 0}} {
		if got, share := speculation(c.drafted, c.accepted); got != c.want || share != c.share {
			t.Errorf("%d drafted, %d kept: %s %.2f, want %s %.2f", c.drafted, c.accepted, got, share, c.want, c.share)
		}
	}
}

func TestRunsFromTheRuntimesReport(t *testing.T) {
	for state, want := range map[string]string{
		contracts.AccelerationGPU: "full", contracts.AccelerationPartial: "partial",
		contracts.AccelerationCPU: "cpu", contracts.AccelerationCPUExpected: "cpu",
	} {
		if got, _ := runsFrom(&contracts.Acceleration{State: state}); got != want {
			t.Errorf("%s: %s, want %s", state, got, want)
		}
	}
}
