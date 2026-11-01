package llamacpp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// ProcessSupervisor manages llama-server child processes.
type ProcessSupervisor struct {
	logsDir string
	mu      sync.Mutex
	procs   map[string]*managedProcess
	onExit  func(instanceID, modelID string, exitCode int, stderrTail string)
}

type managedProcess struct {
	cmd       *exec.Cmd
	endpoint  string
	modelID   string
	adapters  []string
	mode      string
	projector string
	draft     string
	// context is the chat window it was started with; 0 for embedding
	// and reranking.
	context     int
	logFile     *os.File
	logPath     string
	intentional bool
	// acceleration is where llama-server put the model, read from its log
	// once it answered.
	acceleration *pluginapi.Acceleration
	// exited is closed when the process ends.
	exited chan struct{}
}

// NewSupervisor creates a process supervisor.
func NewSupervisor(logsDir string) *ProcessSupervisor {
	return &ProcessSupervisor{
		logsDir: logsDir,
		procs:   make(map[string]*managedProcess),
	}
}

func (r *Runtime) ensureSupervisor() {
	if r.sup == nil {
		r.sup = NewSupervisor(r.logsDir)
	}
}

// OnProcessExit reports a llama-server that exited on its own.
func (r *Runtime) OnProcessExit(fn func(instanceID, modelID string, exitCode int, stderrTail string)) {
	r.ensureSupervisor()
	r.sup.mu.Lock()
	r.sup.onExit = fn
	r.sup.mu.Unlock()
}

func (r *Runtime) StartModel(ctx context.Context, cfg pluginapi.ModelStartConfig) (pluginapi.RunningModel, error) {
	r.installMu.RLock()
	defer r.installMu.RUnlock()
	det, err := r.Detect(ctx)
	if err != nil {
		return pluginapi.RunningModel{}, err
	}
	if !det.Installed {
		return pluginapi.RunningModel{}, contracts.Errorf("RUNTIME_NOT_INSTALLED", map[string]any{"runtime": "llama-server"}, "llama-server not installed: %s", det.Message)
	}
	if cfg.ModelPath == "" {
		return pluginapi.RunningModel{}, fmt.Errorf("model path required")
	}

	// Reuse an already-running instance for the same model. LoRA adapters are
	// fixed when llama-server starts, so an instance without the requested
	// adapters is replaced.
	wantAdapters := adapterIDs(cfg.Adapters)
	supported := cfg.Mode == "" && supportsSpeculation(ctx, det.Path)
	wantDraft := draftFor(cfg, supported)
	if running, err := r.ListRunning(ctx); err == nil {
		for _, m := range running {
			if m.ModelID != cfg.ModelID || m.Status != "running" {
				continue
			}
			// A projector and a draft are loaded at start too: a model
			// started before either was downloaded is started again with it.
			if m.Mode == cfg.Mode && m.Projector == cfg.Projector && m.Draft == wantDraft && sameAdapters(m.Adapters, wantAdapters) && sameContext(m, cfg) {
				return m, nil
			}
			_ = r.StopModel(ctx, m.ID)
		}
	}
	for _, a := range cfg.Adapters {
		if _, err := os.Stat(a.Path); err != nil {
			return pluginapi.RunningModel{}, fmt.Errorf("adapter %s: %w", a.ID, err)
		}
	}
	if cfg.Projector != "" {
		if _, err := os.Stat(cfg.Projector); err != nil {
			return pluginapi.RunningModel{}, fmt.Errorf("image projector: %w", err)
		}
	}

	port := cfg.Port
	if port == 0 {
		port = freePort()
	}
	instanceID := uuid.NewString()
	logPath := filepath.Join(r.logsDir, "llamacpp-"+instanceID+".log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return pluginapi.RunningModel{}, err
	}

	args := startArgs(cfg, port)
	args = append(args, gpuLayerArgs(cfg.GPULayers, supportsFit(ctx, det.Path))...)
	args = append(args, speculativeArgs(cfg, supported)...)
	for _, a := range cfg.Adapters {
		args = append(args, "--lora", a.Path)
	}
	if len(cfg.Adapters) > 0 {
		args = append(args, "--lora-init-without-apply")
	}

	cmd := exec.Command(det.Path, args...)
	cmd.Dir = filepath.Dir(det.Path)
	// Builds from late 2026 log the devices, offloaded layers, and GPU
	// buffers only at trace verbosity, and readAcceleration needs them. An
	// environment variable, unlike a flag, is ignored by older builds.
	cmd.Env = append(os.Environ(), "LLAMA_ARG_LOG_VERBOSITY=4")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	configureLlamaProcess(cmd)
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return pluginapi.RunningModel{}, fmt.Errorf("start llama-server: %w", err)
	}

	endpoint := fmt.Sprintf("http://127.0.0.1:%d", port)
	r.ensureSupervisor()
	r.sup.mu.Lock()
	r.sup.procs[instanceID] = &managedProcess{
		cmd:       cmd,
		endpoint:  endpoint,
		modelID:   cfg.ModelID,
		adapters:  wantAdapters,
		mode:      cfg.Mode,
		projector: cfg.Projector,
		draft:     wantDraft,
		context:   chatContext(cfg),
		logFile:   logFile,
		logPath:   logPath,
		exited:    make(chan struct{}),
	}
	exited := r.sup.procs[instanceID].exited
	r.sup.mu.Unlock()
	registerAdapters(endpoint, wantAdapters)
	registerControl(endpoint, cfg.ModelPath)

	go r.sup.watch(instanceID)

	if err := waitReady(ctx, endpoint, 120*time.Second, exited); err != nil {
		_ = r.StopModel(ctx, instanceID)
		if ctx.Err() != nil {
			return pluginapi.RunningModel{}, err
		}
		return pluginapi.RunningModel{}, pluginapi.LoadFailed(err)
	}

	// llama-server logs where it put the model before it answers.
	acc := readAcceleration(logPath)
	r.sup.mu.Lock()
	if p, ok := r.sup.procs[instanceID]; ok {
		p.acceleration = acc
	}
	r.sup.mu.Unlock()

	return pluginapi.RunningModel{
		ID:           instanceID,
		ModelID:      cfg.ModelID,
		Endpoint:     endpoint,
		Status:       "running",
		RuntimeID:    runtimeID,
		Adapters:     wantAdapters,
		Mode:         cfg.Mode,
		Projector:    cfg.Projector,
		Draft:        wantDraft,
		Acceleration: acc,
	}, nil
}

// startArgs are llama-server's model, address, and mode flags.
func startArgs(cfg pluginapi.ModelStartConfig, port int) []string {
	args := []string{
		"-m", cfg.ModelPath,
		"--host", "127.0.0.1",
		"--port", fmt.Sprintf("%d", port),
	}
	switch cfg.Mode {
	case pluginapi.ModeEmbedding, pluginapi.ModeReranking:
		// Embedding and reranking read each input in one batch, so the batch
		// is as large as the window. Supporting models are small; a short
		// window keeps them light.
		n := cfg.Context
		if n <= 0 || n > supportContext {
			n = supportContext
		}
		flag := "--embedding"
		if cfg.Mode == pluginapi.ModeReranking {
			flag = "--reranking"
		}
		return append(args, flag,
			"--ctx-size", fmt.Sprintf("%d", n),
			"--batch-size", fmt.Sprintf("%d", n),
			"--ubatch-size", fmt.Sprintf("%d", n))
	}
	if cfg.Projector != "" {
		args = append(args, "--mmproj", cfg.Projector)
	}
	// One request at a time, each with the whole window. By default
	// llama-server runs four slots that share one window's worth of memory,
	// yet tells each it has all of it, so two requests at once (a chat's
	// turn and its summary, or Deliberate's drafts) that add up to more than
	// the window both fail with "Context size has been exceeded" (#834).
	// Every room and compaction decision here counts on the whole window.
	// Requests to the same model queue in llama-server instead.
	window := defaultContext(cfg.Context)
	args = append(args, kvCacheArgs(window)...)
	return append(args, "--ctx-size", fmt.Sprintf("%d", window), "--parallel", "1")
}

// LongWindow is the largest window a chat model's key/value cache stays in
// llama.cpp's default f16 for. Past it, the cache is q8_0 (#962), which holds
// a window in 17/32 of the memory (on an Apple M5 Pro, Llama 3.2 1B's 32K
// cache was 544 MiB, not 1,024), with flash attention, which llama-server
// turns on for it, and answers as well in the quality set. models'
// fit estimate counts the same.
const LongWindow = 32768

// kvCacheArgs set a chat model's key/value cache type: q8_0 past LongWindow,
// else llama-server's default f16. TOSKAR_KV_CACHE sets one for every window,
// for measuring: q8_0 or q4_0, or f16 to keep the default even past it.
func kvCacheArgs(window int) []string {
	switch t := strings.ToLower(strings.TrimSpace(config.Env("KV_CACHE"))); t {
	case "q8_0", "q4_0":
		return []string{"--cache-type-k", t, "--cache-type-v", t}
	case "f16":
		return nil
	}
	if window > LongWindow {
		return []string{"--cache-type-k", "q8_0", "--cache-type-v", "q8_0"}
	}
	return nil
}

// supportContext is the window for embedding and reranking models. Mimir
// passages are about 1,200 characters, well within it.
const supportContext = 2048

func (r *Runtime) StopModel(ctx context.Context, id string) error {
	r.ensureSupervisor()
	r.sup.mu.Lock()
	p, ok := r.sup.procs[id]
	if ok {
		p.intentional = true
		delete(r.sup.procs, id)
	}
	r.sup.mu.Unlock()
	if !ok {
		return fmt.Errorf("instance %q not found", id)
	}
	stopLlamaProcess(p.cmd, 1500*time.Millisecond)
	registerAdapters(p.endpoint, nil)
	forgetControl(p.endpoint)
	if p.logFile != nil {
		_ = p.logFile.Close()
	}
	return nil
}

func (r *Runtime) ListRunning(ctx context.Context) ([]pluginapi.RunningModel, error) {
	r.ensureSupervisor()
	r.sup.mu.Lock()
	defer r.sup.mu.Unlock()
	out := make([]pluginapi.RunningModel, 0, len(r.sup.procs))
	for id, p := range r.sup.procs {
		status := "running"
		if p.cmd.ProcessState != nil && p.cmd.ProcessState.Exited() {
			status = "exited"
		}
		out = append(out, pluginapi.RunningModel{
			ID:           id,
			ModelID:      p.modelID,
			Endpoint:     p.endpoint,
			Status:       status,
			RuntimeID:    runtimeID,
			Adapters:     append([]string(nil), p.adapters...),
			Mode:         p.mode,
			Projector:    p.projector,
			Draft:        p.draft,
			Context:      p.context,
			Acceleration: p.acceleration,
		})
	}
	return out, nil
}

func (r *Runtime) Health(ctx context.Context) error {
	det, err := r.Detect(ctx)
	if err != nil {
		return err
	}
	if !det.Installed {
		return contracts.Errorf("RUNTIME_NOT_INSTALLED", map[string]any{"runtime": "llama-server"}, "llama-server not installed: %s", det.Message)
	}
	return nil
}

func (ps *ProcessSupervisor) watch(instanceID string) {
	ps.mu.Lock()
	p, ok := ps.procs[instanceID]
	ps.mu.Unlock()
	if !ok {
		return
	}
	err := p.cmd.Wait()
	code := 0
	if p.cmd.ProcessState != nil {
		code = p.cmd.ProcessState.ExitCode()
	}
	if err != nil && code == 0 {
		code = 1
	}
	if p.exited != nil {
		close(p.exited)
	}
	tail := tailFile(p.logPath, 800)
	ps.mu.Lock()
	intentional := p.intentional
	onExit := ps.onExit
	modelID := p.modelID
	if cur, ok := ps.procs[instanceID]; ok && cur == p {
		delete(ps.procs, instanceID)
	}
	ps.mu.Unlock()
	registerAdapters(p.endpoint, nil)
	forgetControl(p.endpoint)
	if p.logFile != nil {
		_ = p.logFile.Close()
	}
	if !intentional && onExit != nil {
		onExit(instanceID, modelID, code, tail)
	}
}

// Probe asks llama-server whether it is still answering. It does not run inference.
func Probe(ctx context.Context, endpoint string) error {
	client := &http.Client{Timeout: 3 * time.Second}
	base := strings.TrimRight(endpoint, "/")
	if err := probeURL(ctx, client, base+"/health"); err == nil {
		return nil
	}
	return probeURL(ctx, client, base+"/v1/models")
}

func probeURL(ctx context.Context, client *http.Client, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

func tailFile(path string, limit int) string {
	if path == "" || limit <= 0 {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ""
	}
	start := int64(0)
	if info.Size() > int64(limit) {
		start = info.Size() - int64(limit)
	}
	buf := make([]byte, info.Size()-start)
	_, _ = f.ReadAt(buf, start)
	return string(buf)
}

func freePort() int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 8080
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// chatContext is the window a chat model starts with: the one asked for
// (#962), else 8192. Embedding and reranking size theirs in startArgs.
func chatContext(cfg pluginapi.ModelStartConfig) int {
	if cfg.Mode != "" {
		return 0
	}
	return defaultContext(cfg.Context)
}

// sameContext reports whether a running instance has the window a start
// asks for. A start that asks for none takes it as it runs.
func sameContext(m pluginapi.RunningModel, cfg pluginapi.ModelStartConfig) bool {
	return cfg.Context <= 0 || cfg.Mode != "" || m.Context == chatContext(cfg)
}

func defaultContext(c int) int {
	if c <= 0 {
		return 8192
	}
	return c
}

// ContextWindow is the window to count on for a model that isn't running
// here: 8192, or its catalog window when that's smaller. One that runs here
// reports its real window (Window), which Auto may have made bigger (#962);
// one on a paired computer picks its own there, so this stays the floor.
func ContextWindow(catalog int) int {
	running := defaultContext(0)
	if catalog <= 0 || catalog > running {
		return running
	}
	return catalog
}

// gpuLayerArgs puts the model on the graphics card (#955). A build with
// --fit places as many layers as fit the card's free memory, a draft's
// too, and keeps the rest in system memory, so a model bigger than the
// card runs instead of failing to load. It keeps the -c Core sets. Setting -ngl turns that
// off, so it's set only when the start asks for a number, or for an older
// build without --fit, which gets every layer on the card as before.
func gpuLayerArgs(layers int, fits bool) []string {
	if layers > 0 {
		return []string{"-ngl", fmt.Sprintf("%d", layers)}
	}
	if fits {
		return nil
	}
	return []string{"-ngl", fmt.Sprintf("%d", defaultGPULayers())}
}

// supportsFit reports whether the llama-server at path fits a model to the
// card's free memory by itself (#955).
func supportsFit(ctx context.Context, path string) bool {
	return strings.Contains(serverHelp(ctx, path), "--fit-target")
}

func defaultGPULayers() int {
	// Prefer GPU offload when available; llama-server ignores unsupported -ngl.
	switch runtime.GOOS {
	case "darwin":
		return 99
	default:
		return 99
	}
}

// errExitedWhileLoading is returned when llama-server ends before it is
// ready, for example because the model file is damaged or does not fit.
var errExitedWhileLoading = errors.New("llama-server stopped while loading the model")

// waitReady polls until llama-server answers. It returns at once if the
// process exits, instead of waiting out the timeout.
func waitReady(ctx context.Context, endpoint string, timeout time.Duration, exited <-chan struct{}) error {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-exited:
			return errExitedWhileLoading
		default:
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/health", nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		// Fallback for builds without /health.
		req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/v1/models", nil)
		resp2, err2 := client.Do(req2)
		if err2 == nil {
			_, _ = io.Copy(io.Discard, resp2.Body)
			resp2.Body.Close()
			if resp2.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-exited:
			return errExitedWhileLoading
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("llama-server did not become ready at %s", endpoint)
}
