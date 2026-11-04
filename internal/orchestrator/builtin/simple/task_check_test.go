package simple

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/turnopts"
)

func TestCheckCommand(t *testing.T) {
	for command, want := range map[string]bool{
		"go test ./...":                 true,
		"cd sub && go test ./...":       true,
		"CI=1 npm test":                 true,
		"pnpm run build":                true,
		"python3 -m pytest -q":          true,
		"make -j4 test":                 true,
		"./gradlew test":                true,
		"go test ./... 2>&1 | tail -20": true,
		"cargo check":                   true,
		"grep \"go test\" README.md":    false,
		"echo pytest":                   false,
		"ls tests":                      false,
		"cat Makefile":                  false,
		"go version":                    false,
	} {
		if got := checkCommandRe.MatchString(command); got != want {
			t.Errorf("%q: %v", command, got)
		}
	}
}

func TestTestFile(t *testing.T) {
	for path, want := range map[string]bool{
		"sum_test.go": true, "pkg/a_test.py": true, "test_a.py": true, "web/a.test.tsx": true, "b.spec.ts": true,
		"tests/quality/x.go": true, "src/__tests__/y.js": true, "FooTest.java": true,
		"sum.go": false, "latest.go": false, "contest/a.go": false, "attest.py": false,
	} {
		if got := testFileRe.MatchString(path); got != want {
			t.Errorf("%q: %v", path, got)
		}
	}
}

func step(c *taskCheck, command string, exit int, stdout string) {
	c.step(context.Background(), "terminal", map[string]any{"command": command}, map[string]any{"exit": exit, "stdout": stdout}, nil)
}

func TestDoubts(t *testing.T) {
	ctx := context.Background()
	has := func(c *taskCheck, want string) {
		t.Helper()
		got := strings.Join(c.doubts(ctx), " ")
		if want == "" && got != "" || !strings.Contains(got, want) {
			t.Errorf("doubts %q, want %q", got, want)
		}
	}
	c := newTaskCheck(ctx)
	has(c, "")
	step(c, "ls", 0, "")
	has(c, "")

	step(c, "go test ./...", 1, "FAIL")
	has(c, "failed, and it hasn't passed since")
	step(c, "go test ./...", 0, "ok")
	has(c, "")

	c.step(ctx, "filesystem.write", map[string]any{"path": "sum.go"}, nil, nil)
	has(c, "Files changed after the last check")
	step(c, "go test ./...", 0, "ok")
	has(c, "")

	// A pipe's exit code isn't the check's.
	step(c, "go test ./... | tail -5", 0, "--- FAIL: TestAdd\nFAIL\n")
	has(c, "failed")
	step(c, "go test ./... | tail -5", 0, "ok  sum 0.1s\n")
	has(c, "")

	// A check Toskar wasn't allowed to run doesn't count.
	c = newTaskCheck(ctx)
	c.step(ctx, "filesystem.write", map[string]any{"path": "sum.go"}, nil, nil)
	c.step(ctx, "terminal", map[string]any{"command": "go test ./..."}, nil, fmt.Errorf("declined"))
	has(c, "Nothing ran to check")

	c.step(ctx, "filesystem.write", map[string]any{"path": "sum_test.go"}, nil, nil)
	has(c, "`sum_test.go`")
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@example.com", "-c", "user.name=t"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// A shell command can change a test without the file tool: the project shows it,
// committed or not.
func TestDoubtsSeeTheProject(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("sum.go", "package a\n")
	write("sum_test.go", "package a\n")
	write("left.go", "package a\n")
	gitIn(t, dir, "init", "-q")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "first")
	// Changed before the task began: not the task's.
	write("left.go", "package a // mine\n")
	ctx := turnopts.WithProject(context.Background(), dir)

	c := newTaskCheck(ctx)
	if got := c.doubts(ctx); len(got) != 0 {
		t.Fatalf("nothing happened yet: %v", got)
	}
	write("sum.go", "package a // fixed\n")
	if got := strings.Join(c.doubts(ctx), " "); !strings.Contains(got, "Nothing ran to check") || strings.Contains(got, "left.go") {
		t.Fatalf("doubts %q", got)
	}
	step(c, "go test ./...", 0, "ok")
	if got := c.doubts(ctx); len(got) != 0 {
		t.Fatalf("checked: %v", got)
	}
	write("sum.go", "package a // and again\n")
	if got := strings.Join(c.doubts(ctx), " "); !strings.Contains(got, "Files changed after") {
		t.Fatalf("doubts %q", got)
	}
	write("sum_test.go", "package a // weakened\n")
	step(c, "go test ./...", 0, "ok")
	if got := strings.Join(c.doubts(ctx), " "); !strings.Contains(got, "`sum_test.go`") {
		t.Fatalf("doubts %q", got)
	}

	// Committing doesn't hide it.
	c = newTaskCheck(ctx)
	write("sum_test.go", "package a // weakened more\n")
	gitIn(t, dir, "add", "sum_test.go")
	gitIn(t, dir, "commit", "-qm", "tests")
	if got := strings.Join(c.doubts(ctx), " "); !strings.Contains(got, "`sum_test.go`") {
		t.Fatalf("after a commit: %q", got)
	}
}

// A model that says it's fixed with the check failing goes back once, and what's
// still in doubt is under the answer whatever it says.
func TestFalseClaimIsCaught(t *testing.T) {
	failing := &exitEnv{}
	_, answer, prompts := runTaskWith(t, failing, "Fix the failing test", taskProfile(),
		call("go test ./..."), "Fixed: all tests pass now.", "Yes, it is all fixed.")
	if !strings.Contains(answer, "Toskar noticed: The last check, `go test ./...`, failed") {
		t.Fatalf("answer %q", answer)
	}
	if !strings.Contains(strings.Join(prompts, "\n"), "Before you finish:") {
		t.Fatalf("not sent back: %v", prompts)
	}
}

// Once it's checked, nothing is added, and the model's not sent back.
func TestCheckedWorkIsLeftAlone(t *testing.T) {
	_, answer, prompts := runTask(t, "terminal", "Fix the failing test", taskProfile(), call("go test ./..."), "Fixed, and the tests pass.")
	if answer != "Fixed, and the tests pass." {
		t.Fatalf("answer %q", answer)
	}
	if strings.Contains(strings.Join(prompts, "\n"), "Before you finish:") {
		t.Fatal("sent back with nothing in doubt")
	}
}
