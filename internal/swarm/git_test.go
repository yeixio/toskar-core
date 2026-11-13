package swarm

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func testRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	run(t, dir, "init", "-q", "-b", "main")
	run(t, dir, "config", "user.name", "Person")
	run(t, dir, "config", "user.email", "person@example.com")
	run(t, dir, "config", "commit.gpgsign", "false")
	for name, content := range files {
		put(t, dir, name, content)
	}
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "start")
	return dir
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func put(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func get(t *testing.T, dir, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// The person's uncommitted changes, new files that aren't ignored among them,
// are in every worktree, and preparing doesn't touch their index, HEAD, or
// stash. A subfolder works in the same place in each worktree, and only its
// changes are applied.
func TestWorkspaceCarriesChanges(t *testing.T) {
	ctx := context.Background()
	root := testRepo(t, map[string]string{"app/main.txt": "one\n", "app/.gitignore": "secret.txt\n", "top.txt": "top\n"})
	put(t, root, "app/main.txt", "one\ntwo\n")
	put(t, root, "app/new.txt", "new\n")
	put(t, root, "app/secret.txt", "ignored\n")
	put(t, root, "top.txt", "top changed\n")
	put(t, root, "app/staged.txt", "staged\n")
	run(t, root, "add", "app/staged.txt")
	head := run(t, root, "rev-parse", "HEAD")
	status := run(t, root, "status", "--porcelain")

	ws, err := Prepare(ctx, filepath.Join(root, "app"), t.TempDir(), "0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Remove(ctx)
	if ws.Root != root || ws.Sub != "app" || ws.Head != head || ws.Base == head || ws.Prefix != "toskar/swarm/01234567/" {
		t.Fatalf("workspace = %+v", ws)
	}
	if run(t, root, "rev-parse", "HEAD") != head || run(t, root, "status", "--porcelain") != status || run(t, root, "stash", "list") != "" {
		t.Fatal("preparing changed the person's repository")
	}
	top, folder, branch, err := ws.Add(ctx, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	if folder != filepath.Join(top, "app") || branch != "toskar/swarm/01234567/agent-1" {
		t.Fatalf("folder %q, branch %q", folder, branch)
	}
	if get(t, folder, "main.txt") != "one\ntwo\n" || get(t, folder, "new.txt") != "new\n" || get(t, folder, "staged.txt") != "staged\n" || get(t, top, "top.txt") != "top changed\n" {
		t.Fatal("the worktree lacks the person's changes")
	}
	if _, err := os.Stat(filepath.Join(folder, "secret.txt")); !os.IsNotExist(err) {
		t.Error("an ignored file was carried")
	}

	// An agent's change, committed as Toskar, is applied as uncommitted
	// changes in the folder only.
	put(t, folder, "main.txt", "one\ntwo\nthree\n")
	put(t, top, "top.txt", "outside the folder\n")
	if changed, err := ws.Commit(ctx, top, "Agent 1: more"); err != nil || !changed {
		t.Fatalf("commit: %v %v", changed, err)
	}
	if got := run(t, top, "log", "-1", "--format=%an|%s"); got != "Toskar|Agent 1: more" {
		t.Errorf("commit = %q", got)
	}
	if files := ws.Files(ctx, branch); strings.Join(files, ",") != "app/main.txt" {
		t.Errorf("files = %v", files)
	}
	patch, err := ws.Diff(ctx, branch)
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.Apply(ctx, patch); err != nil {
		t.Fatal(err)
	}
	if get(t, root, "app/main.txt") != "one\ntwo\nthree\n" || get(t, root, "top.txt") != "top changed\n" {
		t.Error("the apply didn't stay in the folder")
	}
	if run(t, root, "rev-parse", "HEAD") != head || run(t, root, "diff", "--cached", "--name-only") != "app/staged.txt" {
		t.Error("the apply touched HEAD or the index")
	}

	// Removing leaves the branch, and DeleteBranches deletes it.
	ws.Remove(ctx)
	if strings.Count(run(t, root, "worktree", "list", "--porcelain"), "worktree ") != 1 {
		t.Error("a worktree is left")
	}
	if n, err := DeleteBranches(ctx, root, "0123456789abcdef"); err != nil || n != 1 {
		t.Fatalf("deleted %d, %v", n, err)
	}
}

// A change that conflicts with what the person did since isn't forced.
func TestApplyDoesNotForce(t *testing.T) {
	ctx := context.Background()
	root := testRepo(t, map[string]string{"a.txt": "a\n"})
	ws, err := Prepare(ctx, root, t.TempDir(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Remove(ctx)
	top, _, branch, _ := ws.Add(ctx, "agent-1")
	put(t, top, "a.txt", "from the agent\n")
	put(t, top, "b.txt", "new\n")
	if _, err := ws.Commit(ctx, top, "Agent 1"); err != nil {
		t.Fatal(err)
	}
	put(t, root, "a.txt", "the person's\n")
	patch, _ := ws.Diff(ctx, branch)
	if ws.CanApply(ctx, patch) {
		t.Fatal("a conflicting change can apply")
	}
	if err := ws.Apply(ctx, patch); !errors.Is(err, ErrApplyConflict) {
		t.Fatalf("apply = %v", err)
	}
	if get(t, root, "a.txt") != "the person's\n" {
		t.Error("the person's change was overwritten")
	}
	if _, err := os.Stat(filepath.Join(root, "b.txt")); !os.IsNotExist(err) {
		t.Error("half the change was applied")
	}
}

// Merging two branches that change the same lines leaves the conflict for a
// fix-up; settled, the merge is committed, and given up, it's aborted.
func TestMergeConflict(t *testing.T) {
	ctx := context.Background()
	root := testRepo(t, map[string]string{"a.txt": "a\n"})
	ws, err := Prepare(ctx, root, t.TempDir(), "s2")
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Remove(ctx)
	var branches []string
	for _, text := range []string{"one\n", "two\n"} {
		top, _, branch, _ := ws.Add(ctx, AgentBranchName(len(branches)+1))
		put(t, top, "a.txt", text)
		if _, err := ws.Commit(ctx, top, "agent"); err != nil {
			t.Fatal(err)
		}
		branches = append(branches, branch)
	}
	top, _, merged, _ := ws.Add(ctx, MergedBranchName)
	if files, err := ws.Merge(ctx, top, branches[0], "first"); err != nil || len(files) != 0 {
		t.Fatalf("first merge: %v %v", files, err)
	}
	files, err := ws.Merge(ctx, top, branches[1], "second")
	if err != nil || strings.Join(files, ",") != "a.txt" {
		t.Fatalf("conflict = %v, %v", files, err)
	}
	if ws.Resolved(ctx, top, branches[1], files) {
		t.Fatal("markers left, yet resolved")
	}
	ws.Abort(ctx, top)
	if get(t, top, "a.txt") != "one\n" {
		t.Fatalf("after abort: %q", get(t, top, "a.txt"))
	}
	if _, err := ws.Merge(ctx, top, branches[1], "again"); err != nil {
		t.Fatal(err)
	}
	put(t, top, "a.txt", "one and two\n")
	if !ws.Resolved(ctx, top, branches[1], files) {
		t.Fatal("settled, yet not resolved")
	}
	if got := ws.Files(ctx, merged); strings.Join(got, ",") != "a.txt" || get(t, top, "a.txt") != "one and two\n" {
		t.Errorf("merged = %v", got)
	}
}

// A folder outside Git, or in a repository with no commit, isn't one to
// branch from.
func TestRepoRoot(t *testing.T) {
	ctx := context.Background()
	if _, ok := RepoRoot(ctx, t.TempDir()); ok {
		t.Error("a plain folder is a repository")
	}
	empty := t.TempDir()
	run(t, empty, "init", "-q")
	if _, ok := RepoRoot(ctx, empty); ok {
		t.Error("a repository with no commit is one to branch from")
	}
}
