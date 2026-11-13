package swarm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// A task swarm in a Git folder (docs/features/swarms.md#folders): each agent
// works in a Git worktree of its own, on a branch of its own, starting from
// what the person sees, and Toskar merges the branches. Nothing here changes
// the person's branch, HEAD, index, stash, or working tree: worktrees and
// branches are Toskar's, and the one change to the person's folder, Apply,
// comes only after they allow it.

// Toskar commits as itself, never as the person.
const (
	gitName  = "Toskar"
	gitEmail = "toskar@localhost"
)

// gitTimeout caps one Git command; a merge or diff of a big repository is
// still quick next to an agent's turn.
const gitTimeout = 2 * time.Minute

// ErrApplyConflict is a swarm's change that no longer applies to the
// person's folder: it changed since the swarm started, in the same places.
var ErrApplyConflict = errors.New("the folder changed since the swarm started, and the change no longer applies")

// git runs a Git command in dir and returns its output, trimmed. Hooks never
// run: the person's hooks are for their commits, not Toskar's. env adds to the
// environment, such as a separate index file.
func git(ctx context.Context, dir string, env []string, stdin []byte, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	full := append([]string{"-C", dir, "-c", "core.hooksPath=" + noHooks, "-c", "commit.gpgsign=false", "-c", "core.quotepath=off"}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C",
		"GIT_AUTHOR_NAME="+gitName, "GIT_AUTHOR_EMAIL="+gitEmail,
		"GIT_COMMITTER_NAME="+gitName, "GIT_COMMITTER_EMAIL="+gitEmail)
	cmd.Env = append(cmd.Env, env...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errOut.String())
		if msg == "" {
			msg = strings.TrimSpace(out.String())
		}
		return strings.TrimSpace(out.String()), fmt.Errorf("git %s: %w: %s", args[0], err, msg)
	}
	return strings.TrimRight(out.String(), "\n"), nil
}

// noHooks is a hooks folder that can't hold any, so no hook runs.
var noHooks = os.DevNull

// RepoRoot is the top folder of the Git repository a folder is in, when it
// has a commit to start from; ok is false for a folder that isn't in one.
func RepoRoot(ctx context.Context, folder string) (string, bool) {
	if folder == "" {
		return "", false
	}
	root, err := git(ctx, folder, nil, nil, "rev-parse", "--show-toplevel")
	if err != nil || root == "" {
		return "", false
	}
	if _, err := git(ctx, root, nil, nil, "rev-parse", "--verify", "-q", "HEAD^{commit}"); err != nil {
		// A repository with no commit yet has nothing to branch from.
		return "", false
	}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	return root, true
}

// BranchPrefix is where a swarm's branches are: toskar/swarm/<short id>/.
func BranchPrefix(swarmID string) string {
	short := strings.ReplaceAll(swarmID, "-", "")
	if len(short) > 8 {
		short = short[:8]
	}
	return "toskar/swarm/" + short + "/"
}

// AgentBranchName is agent n's branch name within a swarm.
func AgentBranchName(n int) string { return fmt.Sprintf("agent-%d", n) }

// MergedBranchName is the branch the agents' work is merged on.
const MergedBranchName = "merged"

// Workspace is a task swarm's Git side: the person's repository, what the
// agents start from, and the worktrees made for them.
type Workspace struct {
	// Root is the repository's top folder. Folder is the person's folder,
	// and Sub its path from Root, "" for the root itself; agents work in
	// the same place in their worktrees, and only changes there are applied.
	Root, Folder, Sub string
	// Head is the person's commit when the swarm started. Base is what the
	// agents start from: Head with the person's uncommitted changes on top,
	// as a commit of Toskar's, or Head when there were none.
	Head, Base string
	// Dir holds the worktrees: <data dir>/swarms/<swarm id>.
	Dir string
	// Prefix is the swarm's branch prefix, from BranchPrefix.
	Prefix string

	mu    sync.Mutex
	trees []string
}

// Prepare opens the person's folder for a task swarm: it records where the
// swarm starts from, with their uncommitted changes, including new files that
// aren't ignored. It reads their repository and adds objects to it, and
// changes nothing they'd see: not their index, HEAD, stash, or files.
func Prepare(ctx context.Context, folder, swarmsDir, swarmID string) (*Workspace, error) {
	root, ok := RepoRoot(ctx, folder)
	if !ok {
		return nil, fmt.Errorf("%s isn't in a Git repository with a commit", folder)
	}
	real := folder
	if r, err := filepath.EvalSymlinks(folder); err == nil {
		real = r
	}
	sub, err := filepath.Rel(root, real)
	if err != nil || strings.HasPrefix(sub, "..") {
		return nil, fmt.Errorf("%s isn't inside %s", folder, root)
	}
	if sub == "." {
		sub = ""
	}
	w := &Workspace{Root: root, Folder: real, Sub: filepath.ToSlash(sub), Dir: filepath.Join(swarmsDir, swarmID), Prefix: BranchPrefix(swarmID)}
	if err := os.MkdirAll(w.Dir, 0o700); err != nil {
		return nil, err
	}
	if w.Head, err = git(ctx, root, nil, nil, "rev-parse", "HEAD"); err != nil {
		return nil, err
	}
	if w.Base, err = w.snapshot(ctx); err != nil {
		return nil, err
	}
	return w, nil
}

// snapshot is a commit of the person's working tree as it is, on top of HEAD,
// or HEAD when nothing is changed. It's made with an index of its own, so
// theirs isn't touched.
func (w *Workspace) snapshot(ctx context.Context) (string, error) {
	index := filepath.Join(w.Dir, "snapshot-index")
	defer os.Remove(index)
	env := []string{"GIT_INDEX_FILE=" + index}
	if _, err := git(ctx, w.Root, env, nil, "read-tree", "HEAD"); err != nil {
		return "", err
	}
	if _, err := git(ctx, w.Root, env, nil, "add", "-A", "--", "."); err != nil {
		return "", err
	}
	tree, err := git(ctx, w.Root, env, nil, "write-tree")
	if err != nil {
		return "", err
	}
	headTree, err := git(ctx, w.Root, nil, nil, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return "", err
	}
	if tree == headTree {
		return w.Head, nil
	}
	return git(ctx, w.Root, nil, nil, "commit-tree", tree, "-p", w.Head, "-m", "Uncommitted changes when the swarm started")
}

// Add makes a worktree named name on a new branch from Base. It returns the
// worktree's top folder, the folder in it that matches the person's, and the
// branch.
func (w *Workspace) Add(ctx context.Context, name string) (top, folder, branch string, err error) {
	top = filepath.Join(w.Dir, name)
	branch = w.Prefix + name
	if _, err := git(ctx, w.Root, nil, nil, "worktree", "add", "-q", "-b", branch, top, w.Base); err != nil {
		return "", "", "", err
	}
	w.mu.Lock()
	w.trees = append(w.trees, top)
	w.mu.Unlock()
	if real, err := filepath.EvalSymlinks(top); err == nil {
		top = real
	}
	folder = top
	if w.Sub != "" {
		folder = filepath.Join(top, filepath.FromSlash(w.Sub))
		_ = os.MkdirAll(folder, 0o755)
	}
	return top, folder, branch, nil
}

// Commit commits everything in a worktree on its branch, as Toskar, and
// reports whether there was anything to commit.
func (w *Workspace) Commit(ctx context.Context, top, message string) (bool, error) {
	if _, err := git(ctx, top, nil, nil, "add", "-A"); err != nil {
		return false, err
	}
	if _, err := git(ctx, top, nil, nil, "diff", "--cached", "--quiet"); err == nil {
		return false, nil
	}
	if _, err := git(ctx, top, nil, nil, "commit", "-q", "--no-verify", "-m", message); err != nil {
		return false, err
	}
	return true, nil
}

// Merge merges a branch into the worktree at top, as its own merge commit.
// On a conflict it returns the conflicted files, and the merge is left in
// progress for a fix-up or Abort.
func (w *Workspace) Merge(ctx context.Context, top, branch, message string) ([]string, error) {
	_, err := git(ctx, top, nil, nil, "merge", "--no-ff", "--no-edit", "--no-verify", "-m", message, branch)
	if err == nil {
		return nil, nil
	}
	conflicted := w.Unmerged(ctx, top)
	if len(conflicted) == 0 {
		return nil, err
	}
	return conflicted, nil
}

// Unmerged are the files a merge left conflicted.
func (w *Workspace) Unmerged(ctx context.Context, top string) []string {
	out, _ := git(ctx, top, nil, nil, "diff", "--name-only", "--diff-filter=U")
	return lines(out)
}

// Resolved reports whether a merge at top is settled: the branch is merged,
// or the merge is still in progress with no file left conflicted or holding a
// conflict marker. A merge still in progress is committed.
func (w *Workspace) Resolved(ctx context.Context, top, branch string, files []string) bool {
	for _, f := range files {
		raw, err := os.ReadFile(filepath.Join(top, filepath.FromSlash(f)))
		if err == nil && HasConflictMarkers(string(raw)) {
			return false
		}
	}
	if _, err := git(ctx, top, nil, nil, "rev-parse", "-q", "--verify", "MERGE_HEAD"); err != nil {
		// No merge in progress: the fix-up committed it, or gave up on it.
		_, err := git(ctx, top, nil, nil, "merge-base", "--is-ancestor", branch, "HEAD")
		return err == nil
	}
	if _, err := git(ctx, top, nil, nil, "add", "-A"); err != nil {
		return false
	}
	if len(w.Unmerged(ctx, top)) > 0 {
		return false
	}
	_, err := git(ctx, top, nil, nil, "commit", "-q", "--no-edit", "--no-verify")
	return err == nil
}

// HasConflictMarkers reports text a merge left markers in.
func HasConflictMarkers(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "<<<<<<< ") || strings.HasPrefix(line, ">>>>>>> ") {
			return true
		}
	}
	return false
}

// Abort gives up a merge in progress at top, back to before it.
func (w *Workspace) Abort(ctx context.Context, top string) {
	if _, err := git(ctx, top, nil, nil, "merge", "--abort"); err != nil {
		_, _ = git(ctx, top, nil, nil, "reset", "-q", "--hard", "HEAD")
	}
}

// pathspec limits a diff to the person's folder.
func (w *Workspace) pathspec() string {
	if w.Sub == "" {
		return "."
	}
	return w.Sub
}

// Diff is the change from Base to a branch in the person's folder, as a patch
// that keeps binary files.
func (w *Workspace) Diff(ctx context.Context, branch string) (string, error) {
	out, err := git(ctx, w.Root, nil, nil, "diff", "--binary", "--no-color", "--no-ext-diff", w.Base, branch, "--", w.pathspec())
	if err != nil {
		return "", err
	}
	if out != "" {
		out += "\n"
	}
	return out, nil
}

// Files are the files a branch changed in the person's folder, from Base.
func (w *Workspace) Files(ctx context.Context, branch string) []string {
	out, _ := git(ctx, w.Root, nil, nil, "diff", "--name-only", "--no-renames", w.Base, branch, "--", w.pathspec())
	return lines(out)
}

// Apply puts a patch from Diff into the person's working tree as uncommitted
// changes; their index and HEAD stay as they are. A patch that doesn't apply
// cleanly, because the folder changed since the swarm started, isn't forced:
// it returns ErrApplyConflict and changes nothing.
func (w *Workspace) Apply(ctx context.Context, patch string) error {
	if strings.TrimSpace(patch) == "" {
		return nil
	}
	if _, err := git(ctx, w.Root, nil, []byte(patch), "apply", "--check", "--whitespace=nowarn", "-"); err != nil {
		return fmt.Errorf("%w (%v)", ErrApplyConflict, err)
	}
	_, err := git(ctx, w.Root, nil, []byte(patch), "apply", "--whitespace=nowarn", "-")
	return err
}

// CanApply reports whether a patch would apply to the person's folder now.
func (w *Workspace) CanApply(ctx context.Context, patch string) bool {
	if strings.TrimSpace(patch) == "" {
		return true
	}
	_, err := git(ctx, w.Root, nil, []byte(patch), "apply", "--check", "--whitespace=nowarn", "-")
	return err == nil
}

// Remove removes the swarm's worktrees and its folder. The branches stay.
func (w *Workspace) Remove(ctx context.Context) {
	w.mu.Lock()
	trees := append([]string(nil), w.trees...)
	w.trees = nil
	w.mu.Unlock()
	for _, top := range trees {
		_, _ = git(ctx, w.Root, nil, nil, "worktree", "remove", "--force", top)
	}
	_ = os.RemoveAll(w.Dir)
	_, _ = git(ctx, w.Root, nil, nil, "worktree", "prune")
}

// DeleteBranches deletes a swarm's kept branches from the repository a folder
// is in, and returns how many it deleted. A branch the person has checked out
// stays.
func DeleteBranches(ctx context.Context, folder, swarmID string) (int, error) {
	root, ok := RepoRoot(ctx, folder)
	if !ok {
		return 0, nil
	}
	_, _ = git(ctx, root, nil, nil, "worktree", "prune")
	out, err := git(ctx, root, nil, nil, "for-each-ref", "--format=%(refname:short)", "refs/heads/"+BranchPrefix(swarmID))
	if err != nil {
		return 0, err
	}
	n := 0
	for _, branch := range lines(out) {
		if _, err := git(ctx, root, nil, nil, "branch", "-q", "-D", branch); err == nil {
			n++
		}
	}
	return n, nil
}

func lines(out string) []string {
	var list []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			list = append(list, l)
		}
	}
	return list
}
