package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/yeixio/toskar-core/internal/turnopts"
)

// A turn's project takes the place of the workspace, for reading and writing,
// and nothing outside it can be reached (#1017).
func TestToolsWorkInTheProject(t *testing.T) {
	root := t.TempDir()
	workspace, project := filepath.Join(root, "workspace"), filepath.Join(root, "project")
	for _, d := range []string{workspace, project} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	ctx := turnopts.WithProject(context.Background(), project)
	if _, err := NewWrite(workspace).Execute(ctx, map[string]any{"path": "a.txt", "content": "hi"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, "a.txt")); err != nil {
		t.Fatal(err)
	}
	got, err := NewRead(workspace).Execute(ctx, map[string]any{"path": "a.txt"})
	if err != nil || got["content"] != "hi" {
		t.Fatalf("%v %v", got, err)
	}
	for _, path := range []string{"../workspace/a.txt", filepath.Join(workspace, "a.txt")} {
		if _, err := NewRead(workspace).Execute(ctx, map[string]any{"path": path}); err == nil {
			t.Errorf("read %s outside the project", path)
		}
		if _, err := NewWrite(workspace).Execute(ctx, map[string]any{"path": path, "content": "x"}); err == nil {
			t.Errorf("wrote %s outside the project", path)
		}
	}
}

func TestSearchLooksInTheProject(t *testing.T) {
	root := t.TempDir()
	workspace, project := filepath.Join(root, "workspace"), filepath.Join(root, "project")
	for dir, name := range map[string]string{workspace: "other.txt", project: "mine.txt"} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := NewSearch(workspace).Execute(turnopts.WithProject(context.Background(), project), map[string]any{"query": "txt"})
	if err != nil {
		t.Fatal(err)
	}
	matches, _ := got["matches"].([]string)
	if len(matches) != 1 || matches[0] != "mine.txt" {
		t.Fatalf("matches %v", got["matches"])
	}
}
