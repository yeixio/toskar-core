package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Checks, as a swarm records them.
const (
	ChecksPassed = "passed"
	ChecksFailed = "failed"
)

// Why a task swarm's change wasn't applied to the person's folder.
const (
	// NotAppliedChanged: the folder changed since the swarm started, and
	// the change conflicts with that.
	NotAppliedChanged = "changed"
	// NotAppliedDeclined: the person said no.
	NotAppliedDeclined = "declined"
	// NotAppliedFailed: applying it failed for another reason.
	NotAppliedFailed = "failed"
)

// ApplyAction is what a task swarm asks the person before it changes their
// folder, as a tool asks before it runs.
const ApplyAction = "swarm.apply"

// TaskOutcome is how a task swarm's merge and delivery went.
type TaskOutcome struct {
	// Folder is the person's folder. Git says it's in a Git repository, so
	// the agents worked in worktrees; otherwise they took turns in it.
	Folder string
	Git    bool
	// Branch has the result: the merged branch, or a race winner's.
	Branch string
	// Merged are the agents whose work is on Branch.
	Merged []int
	// Conflicts are agents' changes that couldn't be merged.
	Conflicts []contracts.SwarmConflict
	// Check is the project's check command, and Checks how it went on
	// Branch: passed, failed, or "" when none ran.
	Check  string
	Checks string
	// Winner is a race's winning agent.
	Winner int
	// Files are the files the change touches in the folder, and
	// AgentFiles each agent's own, by number, as Git saw them: what the
	// agents really changed, whatever they said.
	Files      []string
	AgentFiles map[int][]string
	// Applied says the change is in the person's folder; NotApplied says
	// why it isn't, when there was one to apply.
	Applied    bool
	NotApplied string
}

// Candidate is one race agent's change, for the judge.
type Candidate struct {
	Agent  int
	Title  string
	Diff   string
	Answer string
}

// Judge picks the best of a race's changes, returning its index, or -1 when
// it can't tell.
type Judge func(ctx context.Context, request string, candidates []Candidate) int

// Footer is what Toskar did with a task swarm's change, under the answer
// where the model can't leave it out. English only, as a task's record is.
func (t TaskOutcome) Footer() string {
	var lines []string
	files := func(n int) string {
		if n == 1 {
			return "1 file"
		}
		return fmt.Sprintf("%d files", n)
	}
	if t.Winner > 0 {
		line := fmt.Sprintf("Agent %d's change was kept", t.Winner)
		if t.Checks == ChecksPassed {
			line += ", and its checks passed"
		}
		lines = append(lines, line+".")
	}
	for _, c := range t.Conflicts {
		lines = append(lines, fmt.Sprintf("Agent %d's change wasn't merged: it conflicts with the others' and the fix-up couldn't settle it. It's on the branch `%s`.", c.Agent, c.Branch))
	}
	if t.Check != "" && t.Winner == 0 {
		switch t.Checks {
		case ChecksPassed:
			lines = append(lines, "The checks, `"+t.Check+"`, passed on the merged change.")
		case ChecksFailed:
			lines = append(lines, "The checks, `"+t.Check+"`, failed on the merged change.")
		}
	}
	switch {
	case !t.Git:
	case t.Applied:
		lines = append(lines, fmt.Sprintf("Toskar applied the change to %s as uncommitted changes: %s. It's also on the branch `%s`.", t.Folder, files(len(t.Files)), t.Branch))
	case t.NotApplied == NotAppliedChanged:
		lines = append(lines, fmt.Sprintf("The change wasn't applied: %s changed since the swarm started, and the two conflict. It's on the branch `%s`; run `git merge %s` in the folder to bring it in.", t.Folder, t.Branch, t.Branch))
	case t.NotApplied == NotAppliedDeclined:
		lines = append(lines, fmt.Sprintf("The change wasn't applied. It's on the branch `%s`; run `git merge %s` in the folder to bring it in.", t.Branch, t.Branch))
	case t.NotApplied == NotAppliedFailed:
		lines = append(lines, fmt.Sprintf("The change couldn't be applied. It's on the branch `%s`.", t.Branch))
	case len(t.Files) == 0 && len(t.Conflicts) == 0:
		lines = append(lines, "The agents didn't change any files.")
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n\nToskar noticed: " + strings.Join(lines, " ")
}

// CheckCommand is the command that checks a project in dir: its tests, as
// its own files say how to run them, or "" when Toskar can't tell.
func CheckCommand(dir string) string {
	has := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	switch {
	case has("go.mod"):
		return "go test ./..."
	case has("Cargo.toml"):
		return "cargo test"
	case has("package.json"):
		raw, err := os.ReadFile(filepath.Join(dir, "package.json"))
		if err != nil {
			return ""
		}
		var pkg struct {
			Scripts map[string]string `json:"scripts"`
		}
		if json.Unmarshal(raw, &pkg) != nil {
			return ""
		}
		if test := pkg.Scripts["test"]; test != "" && !strings.Contains(test, "no test specified") {
			return "npm test"
		}
	case has("pytest.ini") || has("conftest.py") || fileHas(filepath.Join(dir, "pyproject.toml"), "[tool.pytest") || fileHas(filepath.Join(dir, "setup.cfg"), "[tool:pytest]"):
		return "pytest"
	case makeTestRe.MatchString(readSmall(filepath.Join(dir, "Makefile"))):
		return "make test"
	}
	return ""
}

var makeTestRe = regexp.MustCompile(`(?m)^test:`)

func readSmall(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) > 1<<20 {
		return ""
	}
	return string(raw)
}

func fileHas(path, text string) bool { return strings.Contains(readSmall(path), text) }

// ListFiles lists a folder's files for the coordinator's look before it
// plans: the ones Git tracks or would add in a repository, else a walk that
// skips hidden and dependency folders. It stops at limit.
func ListFiles(ctx context.Context, folder string, limit int) []string {
	if _, ok := RepoRoot(ctx, folder); ok {
		out, err := git(ctx, folder, nil, nil, "ls-files", "--cached", "--others", "--exclude-standard")
		if err == nil {
			list := lines(out)
			slices.Sort(list)
			list = slices.Compact(list)
			if limit > 0 && len(list) > limit {
				list = list[:limit]
			}
			return list
		}
	}
	var list []string
	_ = filepath.WalkDir(folder, func(path string, d fs.DirEntry, err error) error {
		if err != nil || ctx.Err() != nil {
			return filepath.SkipDir
		}
		name := d.Name()
		if d.IsDir() {
			if path != folder && (strings.HasPrefix(name, ".") || slices.Contains(skipDirs, name)) {
				return filepath.SkipDir
			}
			return nil
		}
		if rel, err := filepath.Rel(folder, path); err == nil {
			list = append(list, filepath.ToSlash(rel))
		}
		if limit > 0 && len(list) >= limit {
			return filepath.SkipAll
		}
		return nil
	})
	return list
}

var skipDirs = []string{"node_modules", "vendor", "target", "dist", "build", "__pycache__", "venv"}
