package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/yeixio/toskar-core/internal/turnopts"
)

type base struct {
	workspace string
}

// dir is where a call runs: the folder the turn works in, as the file tools
// and the terminal use (#1017, #1110), or the tool's own workspace.
func (b base) dir(ctx context.Context) string {
	if project := turnopts.ProjectFrom(ctx); project != "" {
		return project
	}
	return b.workspace
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}

type statusTool struct{ base }

func NewStatus(workspace string) *statusTool { return &statusTool{base{workspace}} }
func (t *statusTool) ID() string             { return "git.status" }
func (t *statusTool) DisplayName() string    { return "Git Status" }
func (t *statusTool) Description() string    { return "Show git status" }
func (t *statusTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	out, err := runGit(ctx, t.dir(ctx), "status", "--short")
	if err != nil {
		return nil, err
	}
	return map[string]any{"output": out}, nil
}

type diffTool struct{ base }

func NewDiff(workspace string) *diffTool { return &diffTool{base{workspace}} }
func (t *diffTool) ID() string           { return "git.diff" }
func (t *diffTool) DisplayName() string  { return "Git Diff" }
func (t *diffTool) Description() string  { return "Show git diff" }
func (t *diffTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	path, _ := args["path"].(string)
	gitArgs := []string{"diff"}
	if path != "" {
		gitArgs = append(gitArgs, path)
	}
	out, err := runGit(ctx, t.dir(ctx), gitArgs...)
	if err != nil {
		return nil, err
	}
	return map[string]any{"output": out}, nil
}

type addTool struct{ base }

func NewAdd(workspace string) *addTool { return &addTool{base{workspace}} }
func (t *addTool) ID() string          { return "git.add" }
func (t *addTool) DisplayName() string { return "Git Add" }
func (t *addTool) Description() string { return "Stage files with git add" }
func (t *addTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	path, _ := args["path"].(string)
	if path == "" {
		path = "."
	}
	out, err := runGit(ctx, t.dir(ctx), "add", path)
	if err != nil {
		return nil, err
	}
	return map[string]any{"output": out}, nil
}

type commitTool struct{ base }

func NewCommit(workspace string) *commitTool { return &commitTool{base{workspace}} }
func (t *commitTool) ID() string             { return "git.commit" }
func (t *commitTool) DisplayName() string    { return "Git Commit" }
func (t *commitTool) Description() string    { return "Create a git commit" }
func (t *commitTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	msg, _ := args["message"].(string)
	if strings.TrimSpace(msg) == "" {
		return nil, fmt.Errorf("message required")
	}
	out, err := runGit(ctx, t.dir(ctx), "commit", "-m", msg)
	if err != nil {
		return nil, err
	}
	return map[string]any{"output": clipOutput(out)}, nil
}

type logTool struct{ base }

func NewLog(workspace string) *logTool { return &logTool{base{workspace}} }
func (t *logTool) ID() string          { return "git.log" }
func (t *logTool) DisplayName() string { return "Git Log" }
func (t *logTool) Description() string { return "Show recent git commits" }
func (t *logTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	gitArgs := []string{"log", "-n", "20", "--oneline"}
	if path, _ := args["path"].(string); strings.TrimSpace(path) != "" {
		gitArgs = append(gitArgs, "--", path)
	}
	out, err := runGit(ctx, t.dir(ctx), gitArgs...)
	if err != nil {
		return nil, err
	}
	return map[string]any{"output": clipOutput(out)}, nil
}

type showTool struct{ base }

func NewShow(workspace string) *showTool { return &showTool{base{workspace}} }
func (t *showTool) ID() string           { return "git.show" }
func (t *showTool) DisplayName() string  { return "Git Show" }
func (t *showTool) Description() string  { return "Show one git commit" }
func (t *showTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	revision, _ := args["revision"].(string)
	if strings.TrimSpace(revision) == "" {
		revision = "HEAD"
	}
	out, err := runGit(ctx, t.dir(ctx), "show", "--stat", revision)
	if err != nil {
		return nil, err
	}
	return map[string]any{"output": clipOutput(out)}, nil
}

func clipOutput(out string) string {
	runes := []rune(out)
	if len(runes) > 8000 {
		return string(runes[:8000]) + "\n…"
	}
	return out
}

type pushTool struct{ base }

func NewPush(workspace string) *pushTool { return &pushTool{base{workspace}} }
func (t *pushTool) ID() string           { return "git.push" }
func (t *pushTool) DisplayName() string  { return "Git Push" }
func (t *pushTool) Description() string  { return "Push commits to the remote" }
func (t *pushTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	remote, _ := args["remote"].(string)
	branch, _ := args["branch"].(string)
	if strings.TrimSpace(remote) == "" {
		remote = "origin"
	}
	gitArgs := []string{"push", remote}
	if strings.TrimSpace(branch) != "" {
		gitArgs = append(gitArgs, branch)
	}
	out, err := runGit(ctx, t.dir(ctx), gitArgs...)
	if err != nil {
		return nil, err
	}
	return map[string]any{"output": out}, nil
}
