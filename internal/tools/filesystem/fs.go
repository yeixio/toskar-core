package filesystem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yeixio/toskar-core/internal/turnopts"
)

type readTool struct {
	workspace string
}

func NewRead(workspace string) *readTool { return &readTool{workspace: workspace} }

func (t *readTool) ID() string          { return "filesystem.read" }
func (t *readTool) DisplayName() string { return "Read File" }
func (t *readTool) Description() string { return "Read a file within the workspace" }

func (t *readTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("path required")
	}
	full, err := t.resolve(ctx, path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, err
	}
	content := string(data)
	if len(content) > 12000 {
		content = content[:12000] + "\n…"
	}
	return map[string]any{"path": full, "content": content}, nil
}

type writeTool struct {
	workspace string
}

func NewWrite(workspace string) *writeTool { return &writeTool{workspace: workspace} }

func (t *writeTool) ID() string          { return "filesystem.write" }
func (t *writeTool) DisplayName() string { return "Write File" }
func (t *writeTool) Description() string { return "Write a file within the workspace" }

func (t *writeTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	if path == "" {
		return nil, fmt.Errorf("path required")
	}
	full, err := t.resolve(ctx, path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return nil, err
	}
	return map[string]any{"path": full, "bytes": len(content)}, nil
}

type searchTool struct {
	workspace string
}

func NewSearch(workspace string) *searchTool { return &searchTool{workspace: workspace} }

func (t *searchTool) ID() string          { return "filesystem.search" }
func (t *searchTool) DisplayName() string { return "Find Files" }
func (t *searchTool) Description() string { return "Search file names in the workspace" }

func (t *searchTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	query, _ := args["query"].(string)
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil, fmt.Errorf("query required")
	}
	root := workspaceFor(ctx, t.workspace)
	if root == "" {
		root = "."
	}
	var matches []string
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || ctx.Err() != nil {
			return ctx.Err()
		}
		name := entry.Name()
		if entry.IsDir() && (name == ".git" || name == "node_modules" || name == "dist") {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		if strings.Contains(strings.ToLower(rel), query) {
			matches = append(matches, rel)
		}
		if len(matches) >= 30 {
			return filepath.SkipAll
		}
		return nil
	})
	return map[string]any{"matches": matches}, nil
}

func (t *readTool) resolve(ctx context.Context, path string) (string, error) {
	return resolveWorkspace(workspaceFor(ctx, t.workspace), path)
}

func (t *writeTool) resolve(ctx context.Context, path string) (string, error) {
	return resolveWorkspace(workspaceFor(ctx, t.workspace), path)
}

// workspaceFor is the folder a turn works in, or the tool's own.
func workspaceFor(ctx context.Context, workspace string) string {
	if project := turnopts.ProjectFrom(ctx); project != "" {
		return project
	}
	return workspace
}

func resolveWorkspace(workspace, path string) (string, error) {
	if workspace == "" {
		workspace = "."
	}
	base, err := filepath.Abs(workspace)
	if err != nil {
		return "", err
	}
	full := path
	if !filepath.IsAbs(path) {
		full = filepath.Join(base, path)
	}
	full, err = filepath.Abs(full)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(full, base+string(os.PathSeparator)) && full != base {
		return "", fmt.Errorf("path escapes workspace")
	}
	return full, nil
}
