package training

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/yeixio/yggdrasil-core/internal/pyenv"
	"github.com/yeixio/yggdrasil-core/pkg/contracts"
)

// Trainer is a fine-tuning backend. The job runner and the UI depend only on
// this interface, so backends can be added without changing either.
type Trainer interface {
	ID() string
	DisplayName() string
	// Supports reports whether this backend can train on the hardware, and
	// why not.
	Supports(hw contracts.HardwareInventory) (bool, string)
	// Environment is the Python environment the backend runs in.
	Environment() pyenv.Spec
	// Run trains an adapter. It blocks until training ends, the context is
	// cancelled, or the backend fails. update is called with each change.
	Run(ctx context.Context, spec RunSpec, update func(Update)) (RunResult, error)
}

// RunSpec is one training run.
type RunSpec struct {
	Python       string
	Env          []string
	WorkDir      string
	Repo         string
	Architecture string
	Hyper        Hyper
	// DataDir holds train.jsonl and valid.jsonl.
	DataDir string
	// AdapterOut is where the GGUF adapter is written.
	AdapterOut string
	LogPath    string
}

// Update is a progress report from a backend.
type Update struct {
	State    State
	Detail   string
	Progress Progress
}

// RunResult is what a finished run produced.
type RunResult struct {
	AdapterPath string
	TrainLoss   *float64
	ValLoss     *float64
}

// ErrCancelled is returned when a run stops because its context ended.
var ErrCancelled = errors.New("training cancelled")

//go:embed scripts/mlx_train.py
var mlxScript []byte

// MLXRequirements pins the MLX trainer environment.
var MLXRequirements = []string{"mlx-lm==0.31.3", "mlx==0.32.3", "gguf==0.19.0", "huggingface-hub==1.33.0"}

// MLX trains LoRA and QLoRA adapters with mlx-lm on Apple Silicon.
type MLX struct{}

func (MLX) ID() string          { return "mlx" }
func (MLX) DisplayName() string { return "MLX (Apple Silicon)" }

func (MLX) Supports(hw contracts.HardwareInventory) (bool, string) {
	if hw.OS != "darwin" || hw.Arch != "arm64" {
		return false, "MLX training needs a Mac with Apple Silicon."
	}
	return true, ""
}

func (MLX) Environment() pyenv.Spec {
	return pyenv.Spec{Name: "trainer-mlx", Requirements: MLXRequirements}
}

func (MLX) Run(ctx context.Context, spec RunSpec, update func(Update)) (RunResult, error) {
	if err := os.MkdirAll(spec.WorkDir, 0o755); err != nil {
		return RunResult{}, err
	}
	script := filepath.Join(spec.WorkDir, "mlx_train.py")
	if err := os.WriteFile(script, mlxScript, 0o644); err != nil {
		return RunResult{}, err
	}
	cfg := map[string]any{
		"repo":         spec.Repo,
		"architecture": spec.Architecture,
		"data_dir":     spec.DataDir,
		"adapter_dir":  filepath.Join(spec.WorkDir, "mlx-adapter"),
		"adapter_out":  spec.AdapterOut,
		"hyper":        spec.Hyper,
	}
	cfgPath := filepath.Join(spec.WorkDir, "config.json")
	raw, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(cfgPath, raw, 0o644); err != nil {
		return RunResult{}, err
	}
	return runScript(ctx, spec, []string{spec.Python, "-u", script, cfgPath}, update)
}

// PEFT will train on NVIDIA GPUs with PyTorch and PEFT. It is registered so
// the UI can say why a CUDA computer is not eligible yet.
type PEFT struct{}

func (PEFT) ID() string          { return "peft" }
func (PEFT) DisplayName() string { return "PyTorch PEFT (NVIDIA)" }

func (PEFT) Supports(hw contracts.HardwareInventory) (bool, string) {
	for _, a := range hw.Accelerators {
		for _, b := range a.Backends {
			if strings.EqualFold(b, "cuda") {
				return false, "Training on NVIDIA GPUs is planned but not available yet."
			}
		}
	}
	return false, "Training needs a Mac with Apple Silicon. NVIDIA support is planned."
}

func (PEFT) Environment() pyenv.Spec { return pyenv.Spec{Name: "trainer-peft"} }

func (PEFT) Run(context.Context, RunSpec, func(Update)) (RunResult, error) {
	return RunResult{}, fmt.Errorf("the PEFT trainer is not available yet")
}

// TrainerFor returns the first backend that supports the hardware.
func TrainerFor(backends []Trainer, hw contracts.HardwareInventory) Trainer {
	for _, t := range backends {
		if ok, _ := t.Supports(hw); ok {
			return t
		}
	}
	if len(backends) > 0 {
		return backends[len(backends)-1]
	}
	return nil
}

// scriptEvent is one "@@ygg" line from a trainer script.
type scriptEvent struct {
	Event         string   `json:"event"`
	Stage         string   `json:"stage"`
	Detail        string   `json:"detail"`
	Message       string   `json:"message"`
	Iter          int      `json:"iter"`
	Iters         int      `json:"iters"`
	TrainLoss     *float64 `json:"train_loss"`
	ValLoss       *float64 `json:"val_loss"`
	TokensPerSec  float64  `json:"tokens_per_sec"`
	ItPerSec      float64  `json:"it_per_sec"`
	PeakMemoryGB  float64  `json:"peak_memory_gb"`
	Bytes         int64    `json:"bytes"`
	Total         int64    `json:"total"`
	Adapter       string   `json:"adapter"`
	Seconds       float64  `json:"seconds"`
	TensorsExport int      `json:"tensors"`
}

const eventPrefix = "@@ygg "

// runScript runs a trainer script, turns its event lines into updates, and
// stops the whole process group when ctx ends.
func runScript(ctx context.Context, spec RunSpec, argv []string, update func(Update)) (RunResult, error) {
	logFile, err := os.OpenFile(spec.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return RunResult{}, err
	}
	defer logFile.Close()

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = spec.WorkDir
	cmd.Env = append(os.Environ(), spec.Env...)
	setProcessGroup(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return RunResult{}, err
	}
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		return RunResult{}, fmt.Errorf("start trainer: %w", err)
	}

	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			stopProcessGroup(cmd, 5*time.Second)
		case <-stopped:
		}
	}()

	var res RunResult
	var scriptErr string
	var prog Progress
	var rate *rateTracker
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, eventPrefix) {
			_, _ = io.WriteString(logFile, line+"\n")
			continue
		}
		var ev scriptEvent
		if json.Unmarshal([]byte(strings.TrimPrefix(line, eventPrefix)), &ev) != nil {
			continue
		}
		switch ev.Event {
		case "stage":
			update(Update{State: State(ev.Stage), Detail: ev.Detail, Progress: prog})
		case "download":
			prog.DownloadBytes, prog.DownloadTotal = ev.Bytes, ev.Total
			update(Update{State: StateLoading, Detail: "Downloading base weights", Progress: prog})
		case "progress":
			if rate == nil {
				rate = &rateTracker{}
			}
			prog.Iter, prog.Iters = ev.Iter, ev.Iters
			prog.TrainLoss = ev.TrainLoss
			prog.TokensPerSec = ev.TokensPerSec
			prog.PeakMemoryGB = ev.PeakMemoryGB
			if spec.Hyper.Iters > 0 && spec.Hyper.Epochs > 0 {
				prog.Epochs = spec.Hyper.Epochs
				prog.Epoch = float64(spec.Hyper.Epochs) * float64(ev.Iter) / float64(spec.Hyper.Iters)
			}
			prog.RemainingSec = rate.remaining(time.Now(), ev.Iter, ev.Iters)
			res.TrainLoss = ev.TrainLoss
			update(Update{State: StateTraining, Detail: "Training", Progress: prog})
		case "val":
			prog.ValLoss = ev.ValLoss
			res.ValLoss = ev.ValLoss
			update(Update{State: StateTraining, Detail: "Training", Progress: prog})
		case "done":
			res.AdapterPath = ev.Adapter
			if ev.TrainLoss != nil {
				res.TrainLoss = ev.TrainLoss
			}
			if ev.ValLoss != nil {
				res.ValLoss = ev.ValLoss
			}
		case "error":
			scriptErr = ev.Message
		}
	}
	waitErr := cmd.Wait()
	close(stopped)
	if ctx.Err() != nil {
		return RunResult{}, ErrCancelled
	}
	if waitErr != nil || res.AdapterPath == "" {
		if scriptErr == "" {
			scriptErr = "the trainer stopped without writing an adapter"
			if waitErr != nil {
				scriptErr = "the trainer exited: " + waitErr.Error()
			}
		}
		return RunResult{}, fmt.Errorf("%s (log: %s)", scriptErr, spec.LogPath)
	}
	return res, nil
}

// rateTracker estimates remaining time from measured iterations. It stays
// silent until enough iterations have passed for the rate to be steady.
type rateTracker struct {
	startAt   time.Time
	startIter int
}

func (r *rateTracker) remaining(now time.Time, iter, iters int) *int {
	if r.startAt.IsZero() {
		r.startAt, r.startIter = now, iter
		return nil
	}
	done := iter - r.startIter
	elapsed := now.Sub(r.startAt).Seconds()
	if done < 3 || elapsed < 5 || iters <= iter {
		return nil
	}
	sec := int(elapsed / float64(done) * float64(iters-iter))
	return &sec
}

// TrainerName names the trainer that can train on hw, or "" when none can.
func (s *Service) TrainerName(hw contracts.HardwareInventory) string {
	for _, t := range s.d.Trainers {
		if ok, _ := t.Supports(hw); ok {
			return t.DisplayName()
		}
	}
	return ""
}
