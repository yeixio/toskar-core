package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/turnopts"
)

func TestStatusAndDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.email", "test@example.com")
	gitCmd(t, dir, "config", "user.name", "Yggdrasil Test")
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	status, err := NewStatus(dir).Execute(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status["output"].(string), "note.txt") {
		t.Fatalf("status=%v", status["output"])
	}

	if _, err := NewAdd(dir).Execute(context.Background(), map[string]any{"path": "note.txt"}); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "commit", "-m", "add note")

	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diff, err := NewDiff(dir).Execute(context.Background(), map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff["output"].(string), "+two") {
		t.Fatalf("diff=%v", diff["output"])
	}

	if _, err := NewCommit(dir).Execute(context.Background(), map[string]any{"message": " "}); err == nil {
		t.Fatal("blank commit message was accepted")
	}
	if _, err := NewAdd(dir).Execute(context.Background(), map[string]any{"path": "note.txt"}); err != nil {
		t.Fatal(err)
	}
	committed, err := NewCommit(dir).Execute(context.Background(), map[string]any{"message": "update note"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(committed["output"].(string), "update note") {
		t.Fatalf("commit=%v", committed["output"])
	}

	logOut, err := NewLog(dir).Execute(context.Background(), map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logOut["output"].(string), "update note") {
		t.Fatalf("log=%v", logOut["output"])
	}
	shown, err := NewShow(dir).Execute(context.Background(), map[string]any{"revision": "HEAD"})
	if err != nil || !strings.Contains(shown["output"].(string), "note.txt") {
		t.Fatalf("show=%v err=%v", shown, err)
	}
	if _, err := NewPush(dir).Execute(context.Background(), map[string]any{"remote": "missing", "branch": "main"}); err == nil {
		t.Fatal("push to a missing remote succeeded")
	}

	if _, err := NewStatus(filepath.Join(dir, "missing")).Execute(context.Background(), nil); err == nil {
		t.Fatal("status in a missing directory succeeded")
	}
}

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Yggdrasil Test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Yggdrasil Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// In a turn with a folder, Git runs there, as the file tools and the
// terminal do, not in the tool's own workspace (#1110).
func TestGitRunsInTheTurnsFolder(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	workspace, project := t.TempDir(), t.TempDir()
	for dir, file := range map[string]string{workspace: "workspace.txt", project: "project.txt"} {
		gitCmd(t, dir, "init")
		if err := os.WriteFile(filepath.Join(dir, file), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	status, err := NewStatus(workspace).Execute(turnopts.WithProject(context.Background(), project), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out := status["output"].(string); !strings.Contains(out, "project.txt") || strings.Contains(out, "workspace.txt") {
		t.Fatalf("status in the turn's folder = %q", out)
	}
	status, err = NewStatus(workspace).Execute(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out := status["output"].(string); !strings.Contains(out, "workspace.txt") {
		t.Fatalf("status with no folder = %q", out)
	}
}
