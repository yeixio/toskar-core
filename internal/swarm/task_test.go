package swarm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestCheckCommand(t *testing.T) {
	for want, files := range map[string]map[string]string{
		"go test ./...": {"go.mod": "module x\n"},
		"cargo test":    {"Cargo.toml": "[package]\n"},
		"npm test":      {"package.json": `{"scripts": {"test": "vitest run"}}`},
		"":              {"package.json": `{"scripts": {"test": "echo \"Error: no test specified\" && exit 1"}}`},
		"pytest":        {"pyproject.toml": "[tool.pytest.ini_options]\n"},
		"make test":     {"Makefile": "build:\n\tgo build\ntest:\n\tgo test\n"},
	} {
		dir := t.TempDir()
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if got := CheckCommand(dir); got != want {
			t.Errorf("%v: %q, want %q", files, got, want)
		}
	}
	if got := CheckCommand(t.TempDir()); got != "" {
		t.Errorf("an empty folder: %q", got)
	}
}

// The footer says what Toskar did with the change, and how to get one it
// didn't apply.
func TestTaskFooter(t *testing.T) {
	applied := TaskOutcome{Folder: "/src/app", Git: true, Branch: "toskar/swarm/ab/merged", Files: []string{"a.go"}, Applied: true,
		Check: "go test ./...", Checks: ChecksPassed,
		Conflicts: []contracts.SwarmConflict{{Agent: 2, Branch: "toskar/swarm/ab/agent-2", Files: []string{"a.go"}}}}
	got := applied.Footer()
	for _, want := range []string{"Agent 2's change wasn't merged", "`toskar/swarm/ab/agent-2`", "`go test ./...`, passed", "applied the change to /src/app as uncommitted changes: 1 file"} {
		if !strings.Contains(got, want) {
			t.Errorf("footer lacks %q: %s", want, got)
		}
	}
	changed := TaskOutcome{Folder: "/src/app", Git: true, Branch: "b", Files: []string{"a", "b"}, NotApplied: NotAppliedChanged}
	if got := changed.Footer(); !strings.Contains(got, "changed since the swarm started") || !strings.Contains(got, "`git merge b`") {
		t.Errorf("changed: %s", got)
	}
	if got := (TaskOutcome{Folder: "/src/app"}).Footer(); got != "" {
		t.Errorf("not Git: %q", got)
	}
	race := TaskOutcome{Git: true, Winner: 2, Checks: ChecksPassed, Branch: "b", Files: []string{"a"}, Applied: true}
	if got := race.Footer(); !strings.Contains(got, "Agent 2's change was kept, and its checks passed") {
		t.Errorf("race: %s", got)
	}
	// A change whose checks failed says so, and that the person chose.
	failed := TaskOutcome{Folder: "/src/app", Git: true, Branch: "b", Files: []string{"a"}, Check: "go test ./...", Checks: ChecksFailed, Applied: true}
	if got := failed.Footer(); !strings.Contains(got, "`go test ./...`, failed") || !strings.Contains(got, "only because you chose to") {
		t.Errorf("failed, applied: %s", got)
	}
	failed.Applied, failed.NotApplied = false, NotAppliedDeclined
	if got := failed.Footer(); !strings.Contains(got, "wasn't applied: its checks failed") || !strings.Contains(got, "`git merge b`") {
		t.Errorf("failed, declined: %s", got)
	}
	failed.Winner = 2
	if got := failed.Footer(); !strings.Contains(got, "Agent 2's change was kept, though its checks, `go test ./...`, failed") {
		t.Errorf("failed race: %s", got)
	}
}

// The coordinator's look lists a Git folder's files, new ones that aren't
// ignored among them, and a plain folder's without hidden or dependency
// folders.
func TestListFiles(t *testing.T) {
	ctx := context.Background()
	root := testRepo(t, map[string]string{"a.go": "a", ".gitignore": "out/\n"})
	put(t, root, "b.go", "b")
	put(t, root, "out/x", "x")
	if got := strings.Join(ListFiles(ctx, root, 0), ","); got != ".gitignore,a.go,b.go" {
		t.Errorf("git: %q", got)
	}
	plain := t.TempDir()
	put(t, plain, "a.txt", "a")
	put(t, plain, ".hidden/b", "b")
	put(t, plain, "node_modules/c", "c")
	put(t, plain, "src/d.txt", "d")
	if got := strings.Join(ListFiles(ctx, plain, 0), ","); got != "a.txt,src/d.txt" {
		t.Errorf("plain: %q", got)
	}
	if got := ListFiles(ctx, plain, 1); len(got) != 1 {
		t.Errorf("limit: %v", got)
	}
}
