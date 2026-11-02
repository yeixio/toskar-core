package terminal

import (
	"context"
	"fmt"
	"os/exec"
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
	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
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
