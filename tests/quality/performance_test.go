package quality

import (
	"encoding/json"
	"math"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// performance is how a model ran on this computer: toskar's benchmark for its
// speed, and the runtime's own report for where it ran. It's beside a row's
// Score and never in it (#1037).
type performance struct {
	TokensPerSecond float64 `json:"tokens_per_second"`
	FirstTokenMs    float64 `json:"first_token_ms"`
	LoadMs          float64 `json:"load_ms,omitempty"`
	GPULayers       int     `json:"gpu_layers"`
	GPULayersTotal  int     `json:"gpu_layers_total"`
	MemoryBytes     uint64  `json:"memory_bytes,omitempty"`
}

// TestPerformance measures TOSKAR_QUALITY_MODEL with the quick benchmark, and
// writes TOSKAR_QUALITY_PERFORMANCE_REPORT.
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
	req := contracts.BenchmarkRequest{ModelIDs: []string{model}, WorkloadIDs: []string{"quick"}, Runs: 3}
	if status := d.do(t, http.MethodPost, "/api/v1/benchmarks", req, &job); status >= 300 {
		t.Fatalf("starting the benchmark: status %d", status)
	}
	for deadline := time.Now().Add(30 * time.Minute); ; time.Sleep(2 * time.Second) {
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
	// A figure that wasn't measured doesn't get published as zero.
	if summary.AvgEvalTokPerSec <= 0 || summary.AvgTTFTMs <= 0 {
		t.Fatalf("the benchmark measured nothing: %+v", summary)
	}
	got := performance{
		TokensPerSecond: round1(summary.AvgEvalTokPerSec),
		FirstTokenMs:    math.Round(summary.AvgTTFTMs),
		LoadMs:          math.Round(summary.LoadMs),
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
