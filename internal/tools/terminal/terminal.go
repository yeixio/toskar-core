package terminal

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/yeixio/toskar-core/internal/turnopts"
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
	out, err := cmd.CombinedOutput()
	result := map[string]any{
		"stdout": string(out),
		"exit":   0,
	}
	if err != nil {
		result["error"] = err.Error()
		if exitErr, ok := err.(*exec.ExitError); ok {
			result["exit"] = exitErr.ExitCode()
		}
	}
	return result, nil
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
