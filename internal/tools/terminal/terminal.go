package terminal

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/yeixio/toskar-core/internal/turnopts"
)

var (
	// stopGrace is how long a stopped command gets to clean up before it and
	// what it started are killed.
	stopGrace = 3 * time.Second
	// outputGrace is how long to wait for output once a command has ended or
	// been stopped: something it left running in the background can hold the
	// output open for as long as it runs.
	outputGrace = 5 * time.Second
)

const (
	// maxOutput is how much of a command's output is kept, half from its
	// start and half from its end. A long build can write far more.
	maxOutput = 4 << 20
	// maxLive is how much output can wait to be shown. A command writing
	// faster than it can be shown loses the oldest.
	maxLive = 32 << 10
	// liveEvery is how often waiting output is sent on.
	liveEvery = 200 * time.Millisecond
)

// Tool runs shell commands (ask by default via policy).
type Tool struct{}

func New() *Tool { return &Tool{} }

func (t *Tool) ID() string          { return "terminal" }
func (t *Tool) DisplayName() string { return "Terminal" }
func (t *Tool) Description() string { return "Run a shell command" }

func (t *Tool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	cmdStr, _ := args["command"].(string)
	if strings.TrimSpace(cmdStr) == "" {
		return nil, fmt.Errorf("command required")
	}
	cmd := shellCommand(ctx, cmdStr)
	cmd.Dir = turnopts.ProjectFrom(ctx)
	stopWithChildren(cmd)
	cmd.WaitDelay = outputGrace
	out := &output{live: turnopts.CommandOutputFrom(ctx)}
	cmd.Stdout = out
	cmd.Stderr = out
	stopShowing := out.showLive()
	err := cmd.Run()
	stopShowing()
	// It ended, and left something running that still has the output open.
	if errors.Is(err, exec.ErrWaitDelay) {
		err = nil
	}
	result := map[string]any{
		"stdout": out.String(),
		"exit":   0,
	}
	if err != nil {
		result["error"] = err.Error()
		if exitErr, ok := err.(*exec.ExitError); ok {
			result["exit"] = exitErr.ExitCode()
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result["error"] = "it ran past the time limit and was stopped"
		}
	}
	return result, nil
}

// output keeps what a command writes, and passes it on as it comes.
type output struct {
	mu      sync.Mutex
	head    []byte
	tail    []byte
	dropped int64
	waiting []byte
	live    func(string)
}

func (o *output) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	rest := p
	if room := maxOutput/2 - len(o.head); room > 0 {
		n := min(room, len(rest))
		o.head = append(o.head, rest[:n]...)
		rest = rest[n:]
	}
	o.tail = append(o.tail, rest...)
	if len(o.tail) > maxOutput {
		cut := len(o.tail) - maxOutput/2
		o.dropped += int64(cut)
		o.tail = append(o.tail[:0], o.tail[cut:]...)
	}
	if o.live != nil {
		o.waiting = append(o.waiting, p...)
		if len(o.waiting) > maxLive {
			o.waiting = append(o.waiting[:0], o.waiting[len(o.waiting)-maxLive:]...)
		}
	}
	return len(p), nil
}

func (o *output) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	text := string(o.head)
	// The end of the output, if the middle was too much to keep. Cutting can
	// land inside a character.
	if o.dropped > 0 {
		text += fmt.Sprintf("\n[%d bytes left out]\n", o.dropped)
	}
	return strings.ToValidUTF8(text+string(o.tail), "\uFFFD")
}

// showLive sends output on as it comes, until the func it returns is called.
func (o *output) showLive() (stop func()) {
	if o.live == nil {
		return func() {}
	}
	quit := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		tick := time.NewTicker(liveEvery)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				o.send(false)
			case <-quit:
				o.send(true)
				return
			}
		}
	}()
	return func() {
		close(quit)
		<-stopped
	}
}

// send passes on what's waiting, keeping back a character only partly written
// unless this is the end.
func (o *output) send(end bool) {
	o.mu.Lock()
	n := len(o.waiting)
	for back := 1; !end && back <= utf8.UTFMax && back <= len(o.waiting); back++ {
		at := len(o.waiting) - back
		if utf8.RuneStart(o.waiting[at]) {
			if !utf8.FullRune(o.waiting[at:]) {
				n = at
			}
			break
		}
	}
	text := strings.ToValidUTF8(string(o.waiting[:n]), "\uFFFD")
	o.waiting = append(o.waiting[:0], o.waiting[n:]...)
	o.mu.Unlock()
	if text != "" {
		o.live(text)
	}
}

// powerShellSetup keeps progress bars out of the output and has it in UTF-8,
// which Windows PowerShell doesn't by default.
const powerShellSetup = "$ProgressPreference='SilentlyContinue';[Console]::OutputEncoding=[Text.Encoding]::UTF8;"

// shellCommand runs a command in sh, or on Windows, where sh is often missing,
// in PowerShell. tools.TerminalDescription tells the model which it's writing for.
func shellCommand(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS != "windows" {
		return exec.CommandContext(ctx, "sh", "-c", command)
	}
	shell := "powershell"
	if _, err := exec.LookPath("pwsh"); err == nil {
		shell = "pwsh"
	}
	return exec.CommandContext(ctx, shell, "-NoProfile", "-NonInteractive", "-Command", powerShellSetup+command)
}
