package simple

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/internal/turnopts"
)

// A model can say a task is done when it isn't, which is the worst way for one
// to fail (#1017): in one test a model edited the failing test and said it had
// fixed the bug. So Toskar keeps its own record of what the task checked and
// changed, from what ran and from the project itself, not from the model.

// checkCommandRe is a command that checks work: tests or a build, at the start
// of a command or after a separator, so that grep "go test" isn't one.
var checkCommandRe = regexp.MustCompile(`(?:^|[;&|(]\s*)(?:\w+=\S*\s+)*(?:` +
	`go (?:test|build|vet)\b|` +
	`(?:npm|pnpm|yarn|bun)(?: run)? (?:test|build|check|typecheck)\b|` +
	`npx (?:jest|vitest|tsc)\b|tsc\b|jest\b|vitest\b|` +
	`pytest\b|python3? -m (?:pytest|unittest)\b|` +
	`cargo (?:test|build|check)\b|` +
	`make (?:\S+ )*(?:test|tests|check|build)\b|` +
	`mvn (?:\S+ )*(?:test|verify)\b|(?:\./)?gradlew? (?:\S+ )*(?:test|check|build)\b|` +
	`dotnet (?:test|build)\b|rspec\b|ctest\b|swift (?:test|build)\b|` +
	`bundle exec (?:rspec|rake test)\b|rake test\b)`)

// hidesExitRe is a command whose exit code may not be its check's: a pipe, a
// sequence, or a fallback.
var hidesExitRe = regexp.MustCompile(`[|;]`)

// failedOutputRe is how checks say they failed, for when the exit code can't.
var failedOutputRe = regexp.MustCompile(`(?m)^\s*(?:--- )?FAIL\b|^FAILED\b|\b[1-9]\d* (?:tests? )?(?:failed|failures?|errors?)\b|\bbuild failed\b|\berror:`)

// testFileRe is a path that holds tests.
var testFileRe = regexp.MustCompile(`(?i)(?:^|/)(?:tests?|__tests__|spec|specs)/|_test\.[a-z]+$|(?:^|/)test_[^/]+$|\.(?:test|spec)\.[a-z]+$|tests?\.(?:java|kt|cs|swift)$`)

// taskCheck is what a task checked and changed.
type taskCheck struct {
	project string
	// before is the project as the task found it, and head its commit.
	before map[string]string
	head   string
	// written are the files the file tool changed, in any folder.
	written map[string]bool
	// command is the last check run and failed says how it ended; atCheck is
	// the project then, and written since says files changed after it.
	command      string
	failed       bool
	atCheck      map[string]string
	writtenSince bool
	// asked says the model has been sent back once about this.
	asked bool
}

func newTaskCheck(ctx context.Context) *taskCheck {
	c := &taskCheck{project: turnopts.ProjectFrom(ctx), written: map[string]bool{}}
	c.before = c.snapshot(ctx)
	c.head = c.git(ctx, "rev-parse", "HEAD")
	return c
}

// git answers a question about the project, or "". It can't change anything.
func (c *taskCheck) git(ctx context.Context, args ...string) string {
	if c.project == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", c.project}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// snapshot is each changed file in the project with what distinguishes this
// version of it, or nil outside a Git project.
func (c *taskCheck) snapshot(ctx context.Context) map[string]string {
	if c.project == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", c.project, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames").Output()
	if err != nil {
		return nil
	}
	files := map[string]string{}
	for _, entry := range strings.Split(string(out), "\x00") {
		if len(entry) < 4 {
			continue
		}
		path := entry[3:]
		version := entry[:2]
		if info, err := os.Stat(filepath.Join(c.project, path)); err == nil {
			version += info.ModTime().String() + "/" + strconv.FormatInt(info.Size(), 10)
		}
		files[path] = version
	}
	return files
}

// step notes a tool call that ran.
func (c *taskCheck) step(ctx context.Context, id string, args map[string]any, result map[string]any, err error) {
	if err != nil {
		return
	}
	switch id {
	case "filesystem.write":
		if path, _ := args["path"].(string); path != "" {
			c.written[path] = true
			c.writtenSince = true
		}
	case "terminal":
		command, _ := args["command"].(string)
		if !checkCommandRe.MatchString(command) {
			return
		}
		c.command = strings.TrimSpace(command)
		c.failed = exitCode(result) != 0
		if !c.failed && hidesExitRe.MatchString(command) {
			stdout, _ := result["stdout"].(string)
			c.failed = failedOutputRe.MatchString(stdout)
		}
		c.atCheck = c.snapshot(ctx)
		c.writtenSince = false
	}
}

func exitCode(result map[string]any) int {
	switch code := result["exit"].(type) {
	case int:
		return code
	case float64:
		return int(code)
	}
	return 0
}

// changedFiles are the files the task changed, sorted.
func (c *taskCheck) changedFiles(ctx context.Context) []string {
	set := map[string]bool{}
	for path := range c.written {
		set[path] = true
	}
	now := c.snapshot(ctx)
	for _, path := range differing(c.before, now) {
		set[path] = true
	}
	// Committed by the task, so no longer showing as changed.
	if c.head != "" {
		if head := c.git(ctx, "rev-parse", "HEAD"); head != "" && head != c.head {
			for _, path := range strings.Fields(c.git(ctx, "diff", "--name-only", c.head, head)) {
				set[path] = true
			}
		}
	}
	files := make([]string, 0, len(set))
	for path := range set {
		files = append(files, path)
	}
	slices.Sort(files)
	return files
}

func differing(a, b map[string]string) []string {
	var out []string
	for path, version := range b {
		if a[path] != version {
			out = append(out, path)
		}
	}
	for path := range a {
		if _, ok := b[path]; !ok {
			out = append(out, path)
		}
	}
	return out
}

// doubts says what the record can't back up, one sentence each: nothing when
// the last check passed and covers every change.
func (c *taskCheck) doubts(ctx context.Context) []string {
	if ctx.Err() != nil {
		return nil
	}
	changed := c.changedFiles(ctx)
	var out []string
	switch {
	case c.command != "" && c.failed:
		out = append(out, "The last check, `"+oneLine(c.command)+"`, failed, and it hasn't passed since.")
	case c.command != "" && (c.writtenSince || len(differing(c.atCheck, c.snapshot(ctx))) > 0):
		out = append(out, "Files changed after the last check, `"+oneLine(c.command)+"`, so it doesn't cover them.")
	case c.command == "" && len(changed) > 0:
		out = append(out, "Nothing ran to check these changes.")
	}
	var tests []string
	for _, path := range changed {
		if testFileRe.MatchString(filepath.ToSlash(path)) {
			tests = append(tests, "`"+path+"`")
		}
	}
	if len(tests) > 0 {
		out = append(out, "This changed test files: "+strings.Join(tests, ", ")+". Tests passing may not mean the problem is fixed.")
	}
	return out
}

// sendBack is what to tell a model finishing with doubts, once, while it can
// still do something about them.
func (c *taskCheck) sendBack(ctx context.Context) string {
	if c.asked {
		return ""
	}
	c.asked = true
	doubts := c.doubts(ctx)
	if len(doubts) == 0 {
		return ""
	}
	return "Before you finish: " + strings.Join(doubts, " ") + " Run the check now if there is one. Fix the code rather than a test unless the person asked to change the test. If it still isn't working or you can't check it, say so plainly."
}

// footer is the record, added under the answer where the model can't leave it
// out. English only, like the report of a task that stopped.
func (c *taskCheck) footer(ctx context.Context) string {
	doubts := c.doubts(ctx)
	if len(doubts) == 0 {
		return ""
	}
	return "\n\nToskar noticed: " + strings.Join(doubts, " ")
}
