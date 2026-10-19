package imagegen

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // DecodeConfig reads the size of an image to edit
	_ "image/png"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/yeixio/toskar-core/internal/remotetools"
)

// Limits.
const (
	maxPrompt   = 2000
	defaultSide = 1024
	minSide     = 256
	maxSide     = 1536
	maxPixels   = 1536 * 1024
	// runLimit allows for a slow computer making a large image on the CPU.
	runLimit = 20 * time.Minute
)

// Engine runs sd-cli.
type Engine struct {
	Setup *Setup
	// WorkDir holds each job's files while it runs.
	WorkDir string
	// What names what it makes in messages: image generation (the
	// default) or video generation.
	What string
	// Room, when set, frees memory for a job needing need bytes before it
	// starts and keeps it free until release is called (#541).
	Room func(ctx context.Context, need int64, what string) (release func(), err error)
	// Logger records what sd-cli printed when a job fails.
	Logger *slog.Logger

	// mu runs one image at a time; each uses all of the memory it can.
	mu sync.Mutex
}

func (e *Engine) what() string {
	if e.What != "" {
		return e.What
	}
	return "image generation"
}

// Available reports whether images can be made, and what would set it up.
func (e *Engine) Available() (bool, string) {
	st := e.Setup.Status()
	if !st.Supported {
		return false, st.Unsupported
	}
	if st.Ready {
		// A model set up before, on a computer that has since less memory
		// to give, or moved here, isn't run: it would fail or freeze it.
		if m, ok := e.Setup.lookup(st.Active); ok && tooLittleMemory(m, st.MemoryBytes) {
			return false, memoryShort(m, st.MemoryBytes)
		}
		return true, ""
	}
	for _, m := range st.Models {
		if m.Recommended && m.TooLittleMemory {
			return false, memoryShort(m.Model, st.MemoryBytes)
		}
		if m.Recommended {
			return false, fmt.Sprintf("%s isn't set up yet. Set it up on the Tools page: %s, %.1f GB to download", e.what(), m.Name, float64(m.SizeBytes)/1e9)
		}
	}
	return false, e.what() + " isn't set up yet. Set it up on the Tools page"
}

// provider describes this computer's image provider (Gungnir §16). Only the
// macOS build uses the GPU (Metal); the others run on the CPU.
func (e *Engine) provider(tool string) remotetools.Provider {
	p := remotetools.Provider{Tool: tool, Accelerated: e.Setup.Accelerated()}
	st := e.Setup.Status()
	if m, ok := e.Setup.lookup(st.Active); ok {
		p.Name = m.Name
	}
	switch {
	case st.Ready:
		p.State = remotetools.Healthy
	case st.Job != nil && st.Job.Running:
		p.State = remotetools.Installing
	case st.Job != nil && st.Job.Error != "":
		p.State, p.Reason = remotetools.Failed, st.Job.Error
	default:
		_, p.Reason = e.Available()
		p.State = remotetools.Unavailable
	}
	return p
}

// Request is one image to make, or to edit when Reference is set.
type Request struct {
	Prompt string
	Width  int
	Height int
	// Seed repeats an image; 0 picks one.
	Seed int64
	// Reference is the image to edit, and RefName its file name.
	Reference []byte
	RefName   string
	// Quality asks for a tier: fast, balanced, or best (#549).
	Quality string
}

// Result is a made image.
type Result struct {
	PNG     []byte
	Width   int
	Height  int
	Seed    int64
	Seconds float64
	Model   string
	// Tier is the quality the model gives, and Missing the model that
	// would give the quality asked for, when it isn't set up.
	Tier    string
	Missing string
}

// side rounds a dimension to what the model takes: a multiple of 64 in range.
func side(n int) int {
	if n <= 0 {
		n = defaultSide
	}
	n = (n + 32) / 64 * 64
	return min(max(n, minSide), maxSide)
}

// fit returns the size to make an image of, keeping the aspect of w×h
// within the pixel budget.
func fit(w, h int) (int, int) {
	w, h = side(w), side(h)
	if w*h > maxPixels {
		scale := math.Sqrt(float64(maxPixels) / float64(w*h))
		w = max(int(float64(w)*scale)/64*64, minSide)
		h = max(int(float64(h)*scale)/64*64, minSide)
	}
	return w, h
}

// Generate makes an image.
func (e *Engine) Generate(ctx context.Context, req Request) (Result, error) {
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return Result{}, fmt.Errorf("prompt required: describe the image")
	}
	if utf8.RuneCountInString(prompt) > maxPrompt {
		return Result{}, fmt.Errorf("the prompt is longer than %d characters", maxPrompt)
	}
	if ok, why := e.Available(); !ok {
		return Result{}, fmt.Errorf("%s", why)
	}
	if err := e.ensureCurrent(ctx); err != nil {
		return Result{}, err
	}
	cli := e.Setup.CLI()
	model, files, missing, err := e.Setup.pathsFor(req.Reference != nil, req.Quality)
	if err != nil {
		return Result{}, err
	}
	w, h := req.Width, req.Height
	if req.Reference != nil {
		if !model.Edits {
			return Result{}, errors.New(editModelMissing(e.Setup))
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(req.Reference))
		if err != nil {
			return Result{}, fmt.Errorf("%s is not a PNG or JPEG image", req.RefName)
		}
		// An edit keeps the picture's shape unless a size is asked for.
		if w == 0 && h == 0 {
			w, h = cfg.Width, cfg.Height
			if w > defaultSide || h > defaultSide {
				scale := float64(defaultSide) / float64(max(w, h))
				w, h = int(float64(w)*scale), int(float64(h)*scale)
			}
		}
	}
	w, h = fit(w, h)
	// A size this computer's graphics can't hold is turned down before it
	// runs, with the size that fits (#550).
	if why := tooBigFor(model, w, h, 1, e.Setup.graphics()); why != "" {
		return Result{}, errors.New(why)
	}
	seed := req.Seed
	if seed <= 0 {
		seed = rand.Int64N(1 << 31)
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	release, err := e.makeRoom(ctx, model, "image")
	if err != nil {
		return Result{}, err
	}
	defer release()
	if err := os.MkdirAll(e.WorkDir, 0o700); err != nil {
		return Result{}, err
	}
	dir, err := os.MkdirTemp(e.WorkDir, "image-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "out.png")
	args := []string{
		"--diffusion-model", files["diffusion"], "--vae", files["vae"], "--llm", files["llm"],
		"-p", prompt, "-W", strconv.Itoa(w), "-H", strconv.Itoa(h),
		"--steps", strconv.Itoa(model.Steps), "--cfg-scale", strconv.FormatFloat(model.CFG, 'f', -1, 64),
		"--sampling-method", "euler", "-s", strconv.FormatInt(seed, 10), "-o", out,
		// Weights wait in memory and move to the GPU when used, so a
		// computer with less graphics memory can still run the model.
		"--offload-to-cpu", "--diffusion-fa",
	}
	if v := files["llm_vision"]; v != "" {
		args = append(args, "--llm_vision", v)
	}
	if model.FlowShift > 0 {
		args = append(args, "--flow-shift", strconv.FormatFloat(model.FlowShift, 'f', -1, 64))
	}
	if model.ModelArgs != "" {
		args = append(args, "--model-args", model.ModelArgs)
	}
	if req.Reference != nil {
		ext := strings.ToLower(filepath.Ext(req.RefName))
		if ext != ".jpg" && ext != ".jpeg" {
			ext = ".png"
		}
		ref := filepath.Join(dir, "reference"+ext)
		if err := os.WriteFile(ref, req.Reference, 0o600); err != nil {
			return Result{}, err
		}
		args = append(args, "-r", ref)
	}
	start := time.Now()
	data, err := e.runWithFallback(ctx, cli, dir, args, out, "image", runLimit)
	if err != nil {
		return Result{}, err
	}
	return Result{PNG: data, Width: w, Height: h, Seed: seed, Seconds: time.Since(start).Seconds(), Model: model.Name,
		Tier: model.Tier, Missing: missing}, nil
}

// runWithFallback runs sd-cli, and when the Vulkan build can't start its
// GPU here, installs the CPU build and runs that instead (#154).
func (e *Engine) runWithFallback(ctx context.Context, cli, dir string, args []string, out, what string, limit time.Duration) ([]byte, error) {
	data, err := runCLI(ctx, cli, dir, args, out, what, limit)
	var failed *cliError
	if errors.As(err, &failed) && e.Logger != nil {
		e.Logger.Warn("sd-cli failed", "what", what, "kind", failed.kind, "output", lastBytes(failed.output, 4<<10))
	}
	if err == nil || failed == nil || !gpuFailure(failed.output) || e.Setup.InstalledBuild() != BuildVulkan {
		return data, err
	}
	if ferr := e.Setup.fallBackToCPU(ctx); ferr != nil {
		return nil, fmt.Errorf("%w; the GPU build couldn't start here, and the CPU build couldn't be installed: %v", err, ferr)
	}
	return runCLI(ctx, e.Setup.CLI(), dir, args, out, what, limit)
}

// cliError is sd-cli failing, with the end of what it printed.
type cliError struct {
	msg    string
	output string
	kind   string
}

func (e *cliError) Error() string { return e.msg }

// Kind is the failure's kind, such as FailureMemory.
func (e *cliError) Kind() string { return e.kind }

func lastBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// gpuStartFailures are what a Vulkan build prints when it can't start on
// the GPU at all: no device, no loader, or a driver that won't initialize.
// Anything else, such as running out of memory or a bad prompt, isn't the
// build's fault, so it doesn't switch to the CPU build.
var gpuStartFailures = []string{
	"no vulkan devices", "ggml_vulkan: no devices", "failed to initialize vulkan", "failed to create vulkan instance",
	"vk_error_initialization_failed", "vk_error_incompatible_driver", "vk_error_layer_not_present", "vk_error_extension_not_present",
	"libvulkan.so.1: cannot open shared object file", "vulkan-1.dll was not found", "vulkan-1.dll not found",
}

// gpuFailure reports output from a Vulkan build that couldn't start on the
// GPU.
func gpuFailure(output string) bool {
	lower := strings.ToLower(output)
	for _, sign := range gpuStartFailures {
		if strings.Contains(lower, sign) {
			return true
		}
	}
	return false
}

// runCLI runs sd-cli in dir and returns the file it wrote to out.
func runCLI(ctx context.Context, cli, dir string, args []string, out, what string, limit time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, cli, args...)
	cmd.Dir = dir
	cmd.WaitDelay = 5 * time.Second
	var stderr tail
	cmd.Stdout, cmd.Stderr = &stderr, &stderr
	// Its progress, for a chat waiting on it (#706).
	if report := progressFrom(ctx); report != nil {
		cmd.Stdout = io.MultiWriter(&stderr, newProgressWriter(report))
	}
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("the %s took longer than %d minutes and was stopped", what, int(limit.Minutes()))
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		kind := failureKind(string(stderr.b), err)
		return nil, &cliError{msg: failureMessage(kind, what, stderr.lastLine(err)), output: string(stderr.b), kind: kind}
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return nil, fmt.Errorf("the %s could not be made: %s", what, stderr.lastLine(err))
	}
	return data, nil
}

// tail keeps the end of sd-cli's output, for its error.
type tail struct {
	b []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	if len(t.b) > 16<<10 {
		t.b = t.b[len(t.b)-8<<10:]
	}
	return len(p), nil
}

func (t *tail) lastLine(err error) string {
	lines := strings.Split(strings.TrimSpace(string(t.b)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		// Progress bars redraw with carriage returns; keep the last frame.
		parts := strings.Split(lines[i], "\r")
		if s := strings.TrimSpace(parts[len(parts)-1]); s != "" {
			if len(s) > 300 {
				s = s[:300]
			}
			return s
		}
	}
	return err.Error()
}
