package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/models"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/swarm"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Task swarms in a folder (#1135) with real Git in temporary folders and
// stub inference: the planner's parts and each agent's steps are scripted,
// and the agents' tool calls run through the real tools and approvals.

var (
	agentNumberRe = regexp.MustCompile(`You are agent (\d+) of`)
	fixupRe       = regexp.MustCompile(`merging agent (\d+)'s change`)
	projectRe     = regexp.MustCompile(`working in the project folder (.+?)\. Commands`)
)

// step is a scripted model reply: a tool call, or text.
func writeStep(path, content string) string {
	args, _ := json.Marshal(map[string]any{"path": path, "content": content})
	return `<tool_call>{"name":"filesystem.write","arguments":` + string(args) + `}</tool_call>`
}

func commandStep(command string) string {
	args, _ := json.Marshal(map[string]any{"command": command})
	return `<tool_call>{"name":"terminal","arguments":` + string(args) + `}</tool_call>`
}

// taskSwarm is an app whose planner splits every task into the given parts
// and whose agents follow their scripts. The person allows every question,
// except an apply when declineApply is set, and each question is kept.
type taskSwarm struct {
	*App
	t     *testing.T
	parts string
	// scripts are each agent's replies in turn, by number; fixup is the
	// fix-up agent's. After its script an agent says it's done.
	scripts map[int][]string
	fixup   []string
	// during runs once when agent n starts, with its folder.
	during map[int]func(folder string)
	once   map[int]*sync.Once

	mu       sync.Mutex
	folders  map[int]string
	order    []int
	asked    []map[string]any
	declined map[string]bool
}

func newTaskSwarm(t *testing.T, parts string) *taskSwarm {
	t.Helper()
	t.Setenv("TOSKAR_STUB_INFERENCE", "1")
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	a, err := New(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })
	a.SwarmSlots = func(string, string) int { return 2 }
	s := &taskSwarm{App: a, t: t, parts: parts, scripts: map[int][]string{}, during: map[int]func(string){},
		once: map[int]*sync.Once{}, folders: map[int]string{}, declined: map[string]bool{}}
	a.StubReply = s.reply
	return s
}

// reply is the stub model. An agent's text can reach it more than once in a
// turn, so what it does once per agent is guarded.
func (s *taskSwarm) reply(_ string, messages []pluginapi.ChatMessage) string {
	system := messages[0].Content
	if strings.Contains(system, "work on one task in a project folder") {
		return s.parts
	}
	first := ""
	steps := 0
	for _, m := range messages[1:] {
		if m.Role == "user" && first == "" {
			first = m.Content
		}
		if m.Role == "assistant" {
			steps++
		}
	}
	folder := ""
	if m := projectRe.FindStringSubmatch(system); m != nil {
		folder = m[1]
	}
	script, n := []string(nil), 0
	switch {
	case fixupRe.MatchString(first):
		script, n = s.fixup, -1
	case agentNumberRe.MatchString(first):
		n, _ = strconv.Atoi(agentNumberRe.FindStringSubmatch(first)[1])
		script = s.scripts[n]
	default:
		return "Merged."
	}
	s.mu.Lock()
	if _, ok := s.once[n]; !ok {
		s.once[n] = &sync.Once{}
	}
	once := s.once[n]
	s.mu.Unlock()
	once.Do(func() {
		s.mu.Lock()
		s.folders[n] = folder
		s.order = append(s.order, n)
		during := s.during[n]
		s.mu.Unlock()
		if during != nil {
			during(folder)
		}
	})
	if steps < len(script) {
		return script[steps]
	}
	if n == -1 {
		return "I settled the conflict."
	}
	return fmt.Sprintf("Agent %d is done.", n)
}

// approve answers every question the swarm asks, as the person, and keeps
// them. It stops when the test ends.
func (s *taskSwarm) approve(ctx context.Context) {
	subID, ch := s.Bus.Subscribe()
	go func() {
		defer s.Bus.Unsubscribe(subID)
		for {
			select {
			case <-ctx.Done():
				return
			case evt := <-ch:
				if evt.Type != events.ToolRequested {
					continue
				}
				s.mu.Lock()
				s.asked = append(s.asked, evt.Payload)
				deny := s.declined[fmt.Sprint(evt.Payload["tool_id"])]
				s.mu.Unlock()
				id, _ := evt.Payload["request_id"].(string)
				_ = s.Tools.Decide(id, !deny, false)
			}
		}
	}()
}

// questions are what the swarm asked about one tool.
func (s *taskSwarm) questions(toolID string) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []map[string]any
	for _, q := range s.asked {
		if q["tool_id"] == toolID {
			out = append(out, q)
		}
	}
	return out
}

// profile makes a profile whose tools write files and run commands with the
// given policies.
func (s *taskSwarm) profile(ctx context.Context, write, terminal string) string {
	s.t.Helper()
	p, err := s.Profiles.Create(ctx, profiles.Profile{Name: "Coder", OrchestratorID: "simple", Roles: []contracts.ModelRole{{Role: "assistant"}},
		Tools: []contracts.ToolPolicy{
			{ToolID: "filesystem.write", Policy: write},
			{ToolID: "filesystem.read", Policy: tools.PolicyAllow},
			{ToolID: "terminal", Policy: terminal},
		}})
	if err != nil {
		s.t.Fatal(err)
	}
	return p.ID
}

// run runs a task swarm turn in folder and returns its answer and the swarm.
func (s *taskSwarm) run(ctx context.Context, profileID, mode, folder, prompt string) (string, contracts.Swarm) {
	s.t.Helper()
	conv, err := s.Conversations.Create(ctx, "Task", profileID, "")
	if err != nil {
		s.t.Fatal(err)
	}
	turn := turnopts.WithProject(swarm.WithMode(ctx, mode), folder)
	stream, err := s.RunChat(turn, "", conv.ID, prompt, true, "", "")
	if err != nil {
		s.t.Fatal(err)
	}
	done := make(chan string, 1)
	go func() {
		var answer string
		for c := range stream {
			if c.Replace {
				answer = ""
			}
			answer += c.Content
		}
		done <- answer
	}()
	select {
	case answer := <-done:
		list, _ := s.Swarms.List(ctx, "")
		if len(list) != 1 {
			s.t.Fatalf("swarms = %+v", list)
		}
		return answer, list[0]
	case <-time.After(60 * time.Second):
		s.t.Fatal("the swarm didn't finish")
	}
	return "", contracts.Swarm{}
}

// repo makes a Git repository with files committed.
func repo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "user.name", "Person")
	gitIn(t, dir, "config", "user.email", "person@example.com")
	gitIn(t, dir, "config", "commit.gpgsign", "false")
	for name, content := range files {
		write(t, dir, name, content)
	}
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "start")
	return dir
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

const goMod = "module example.com/greet\n\ngo 1.21\n"

// Two agents change different files at once in worktrees carrying the
// person's uncommitted changes; both branches merge, the checks pass, and
// after the person allows it their folder gets one uncommitted change. Their
// branch, HEAD, index, and stash stay as they were; the worktrees go, the
// branches stay, and deleting the swarm deletes them.
func TestTaskSwarmSplit(t *testing.T) {
	folder := repo(t, map[string]string{"go.mod": goMod, "notes.txt": "first\n", "greet.go": "package greet\n"})
	// Uncommitted: a tracked file changed, a new file, and a staged one.
	write(t, folder, "notes.txt", "first\nsecond, not committed\n")
	write(t, folder, "draft.txt", "a new file\n")
	write(t, folder, "staged.txt", "staged\n")
	gitIn(t, folder, "add", "staged.txt")
	head := gitIn(t, folder, "rev-parse", "HEAD")
	staged := gitIn(t, folder, "diff", "--cached", "--name-only")

	s := newTaskSwarm(t, `{"parts": [{"title": "Hello", "instructions": "Add Hello.", "paths": ["hello.go"]}, {"title": "Bye", "instructions": "Add Bye.", "paths": ["bye.go"]}]}`)
	s.scripts[1] = []string{writeStep("hello.go", "package greet\n\nfunc Hello() string { return \"hello\" }\n")}
	s.scripts[2] = []string{writeStep("bye.go", "package greet\n\nfunc Bye() string { return \"bye\" }\n")}
	carried := map[int]string{}
	for n := 1; n <= 2; n++ {
		s.during[n] = func(dir string) {
			raw, _ := os.ReadFile(filepath.Join(dir, "notes.txt"))
			extra, _ := os.ReadFile(filepath.Join(dir, "draft.txt"))
			s.mu.Lock()
			carried[n] = string(raw) + "|" + string(extra)
			s.mu.Unlock()
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.approve(ctx)
	profileID := s.profile(ctx, tools.PolicyAsk, tools.PolicyAllow)
	answer, sw := s.run(ctx, profileID, swarm.ModeSplit, folder, "Add a Hello function and a Bye function to the greet package.")

	for n := 1; n <= 2; n++ {
		if carried[n] != "first\nsecond, not committed\n|a new file\n" {
			t.Errorf("agent %d's worktree lacks the person's changes: %q", n, carried[n])
		}
		if s.folders[n] == "" || s.folders[n] == folder || !strings.Contains(s.folders[n], sw.ID) {
			t.Errorf("agent %d worked in %q, not a worktree of its own", n, s.folders[n])
		}
	}
	// The person's folder: both changes, uncommitted, and nothing of theirs lost.
	if !strings.Contains(read(t, folder, "hello.go"), "Hello") || !strings.Contains(read(t, folder, "bye.go"), "Bye") {
		t.Fatal("the merged change isn't in the folder")
	}
	if read(t, folder, "notes.txt") != "first\nsecond, not committed\n" || read(t, folder, "draft.txt") != "a new file\n" {
		t.Error("the person's uncommitted changes were lost")
	}
	if gitIn(t, folder, "rev-parse", "HEAD") != head || gitIn(t, folder, "branch", "--show-current") != "main" {
		t.Error("the person's branch or HEAD moved")
	}
	if got := gitIn(t, folder, "diff", "--cached", "--name-only"); got != staged {
		t.Errorf("the person's index changed: %q, was %q", got, staged)
	}
	if got := gitIn(t, folder, "stash", "list"); got != "" {
		t.Errorf("the stash list changed: %q", got)
	}
	// The apply asked, as a task's writes do, and every agent's write asked.
	applies := s.questions(swarm.ApplyAction)
	if len(applies) != 1 || applies[0]["reason"] != "Apply the swarm's changes to "+folder+": 2 files" || applies[0]["swarm_id"] != sw.ID {
		t.Fatalf("apply questions = %+v", applies)
	}
	writes := s.questions("filesystem.write")
	if len(writes) != 2 {
		t.Fatalf("write questions = %+v", writes)
	}
	for _, q := range writes {
		if q["swarm_id"] != sw.ID || !strings.HasPrefix(fmt.Sprint(q["agent"]), "Agent ") {
			t.Errorf("an agent's question lacks its swarm and agent: %+v", q)
		}
	}
	// The record.
	if sw.Status != swarm.StatusDone || !sw.Applied || sw.Checks != swarm.ChecksPassed || len(sw.Conflicts) != 0 || sw.Folder != folder {
		t.Fatalf("swarm = %+v", sw)
	}
	prefix := swarm.BranchPrefix(sw.ID)
	if sw.Branch != prefix+"merged" || sw.Agents[0].Branch != prefix+"agent-1" || sw.Agents[1].Paths[0] != "bye.go" {
		t.Fatalf("branches = %q, %+v", sw.Branch, sw.Agents)
	}
	if !strings.Contains(answer, "Merged.") || !strings.Contains(answer, "applied the change to "+folder) || !strings.Contains(answer, "`go test ./...`, passed") {
		t.Errorf("answer = %q", answer)
	}
	// Commits are Toskar's, one per agent.
	if got := gitIn(t, folder, "log", "-1", "--format=%an|%s", prefix+"agent-2"); got != "Toskar|Agent 2: Bye" {
		t.Errorf("agent 2's commit = %q", got)
	}
	// Worktrees are gone, branches stay.
	if got := gitIn(t, folder, "worktree", "list", "--porcelain"); strings.Count(got, "worktree ") != 1 {
		t.Errorf("worktrees left:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(s.Config.Get().DataDir, "swarms", sw.ID)); !os.IsNotExist(err) {
		t.Errorf("the swarm's folder is still there: %v", err)
	}
	branches := gitIn(t, folder, "branch", "--list", prefix+"*")
	if strings.Count(branches, prefix) != 3 {
		t.Fatalf("kept branches:\n%s", branches)
	}
	// Deleting the swarm deletes them.
	if err := (&swarmService{app: s.App}).Delete(ctx, sw.ID); err != nil {
		t.Fatal(err)
	}
	if got := gitIn(t, folder, "branch", "--list", prefix+"*"); got != "" {
		t.Errorf("branches left after delete:\n%s", got)
	}
}

// Two agents changing the same lines conflict; the fix-up agent settles it,
// or, when it can't, the conflict is reported with the branch named and the
// other change is still applied.
func TestTaskSwarmConflict(t *testing.T) {
	parts := `{"parts": [{"title": "Hello", "instructions": "Say hello.", "paths": ["greet.txt"]}, {"title": "Hi", "instructions": "Say hi.", "paths": ["other.txt"]}]}`
	for _, fixed := range []bool{true, false} {
		t.Run(fmt.Sprintf("fixed=%v", fixed), func(t *testing.T) {
			folder := repo(t, map[string]string{"greet.txt": "greeting\n"})
			s := newTaskSwarm(t, parts)
			s.scripts[1] = []string{writeStep("greet.txt", "hello\n")}
			s.scripts[2] = []string{writeStep("greet.txt", "hi\n")}
			if fixed {
				s.fixup = []string{writeStep("greet.txt", "hello and hi\n")}
			} else {
				s.fixup = []string{"I couldn't settle it."}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s.approve(ctx)
			answer, sw := s.run(ctx, s.profile(ctx, tools.PolicyAsk, tools.PolicyAllow), swarm.ModeSplit, folder, "Add a greeting in two ways.")
			prefix := swarm.BranchPrefix(sw.ID)
			if len(sw.Agents) != 3 || !strings.Contains(sw.Agents[2].Part, "greet.txt") || sw.Agents[2].Role != "agent:3" {
				t.Fatalf("no fix-up agent in the record: %+v", sw.Agents)
			}
			if s.folders[-1] == "" || !strings.HasSuffix(s.folders[-1], "merged") {
				t.Errorf("the fix-up worked in %q, not the merge's worktree", s.folders[-1])
			}
			if fixed {
				if len(sw.Conflicts) != 0 || read(t, folder, "greet.txt") != "hello and hi\n" || !sw.Applied {
					t.Fatalf("fixed: conflicts %+v, file %q, swarm %+v", sw.Conflicts, read(t, folder, "greet.txt"), sw)
				}
				return
			}
			if len(sw.Conflicts) != 1 || sw.Conflicts[0].Agent != 2 || sw.Conflicts[0].Branch != prefix+"agent-2" || sw.Conflicts[0].Files[0] != "greet.txt" {
				t.Fatalf("conflicts = %+v", sw.Conflicts)
			}
			if !strings.Contains(answer, "Agent 2's change wasn't merged") || !strings.Contains(answer, prefix+"agent-2") {
				t.Errorf("the answer doesn't say what wasn't merged: %q", answer)
			}
			// Agent 1's change still reached the folder, with no conflict markers.
			if got := read(t, folder, "greet.txt"); got != "hello\n" || !sw.Applied {
				t.Errorf("folder = %q, applied %v", got, sw.Applied)
			}
		})
	}
}

// A change that no longer applies, because the person changed the same lines
// while the swarm worked, isn't forced: the folder keeps their edit, and the
// answer names the branch to merge.
func TestTaskSwarmApplyConflict(t *testing.T) {
	folder := repo(t, map[string]string{"greet.txt": "greeting\n"})
	s := newTaskSwarm(t, `{"parts": [{"title": "One", "instructions": "One.", "paths": ["greet.txt"]}, {"title": "Two", "instructions": "Two.", "paths": ["two.txt"]}]}`)
	s.scripts[1] = []string{writeStep("greet.txt", "from the swarm\n")}
	s.scripts[2] = []string{writeStep("two.txt", "two\n")}
	s.during[1] = func(string) { write(t, folder, "greet.txt", "the person's own edit\n") }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.approve(ctx)
	answer, sw := s.run(ctx, s.profile(ctx, tools.PolicyAsk, tools.PolicyAllow), swarm.ModeSplit, folder, "Update the greeting and add two.")
	if read(t, folder, "greet.txt") != "the person's own edit\n" {
		t.Fatalf("the person's edit was overwritten: %q\nanswer %q\nswarm %+v\nasked %+v", read(t, folder, "greet.txt"), answer, sw, s.asked)
	}
	if _, err := os.Stat(filepath.Join(folder, "two.txt")); !os.IsNotExist(err) {
		t.Error("part of the change was applied")
	}
	if sw.Applied || len(s.questions(swarm.ApplyAction)) != 0 {
		t.Errorf("applied %v, asked %v", sw.Applied, s.questions(swarm.ApplyAction))
	}
	if !strings.Contains(answer, "git merge "+sw.Branch) || sw.Branch == "" {
		t.Errorf("the answer doesn't say how to get the change: %q", answer)
	}
}

// In a folder that isn't in Git, the agents take turns in the folder itself,
// and the answer says why. Their writes ask, though the profile allows
// writes, since they're tasks; a destructive command asks though the profile
// allows the terminal.
func TestTaskSwarmNotGit(t *testing.T) {
	folder := t.TempDir()
	if real, err := filepath.EvalSymlinks(folder); err == nil {
		folder = real
	}
	write(t, folder, "old/junk.txt", "junk\n")
	s := newTaskSwarm(t, `{"parts": [{"title": "Clean", "instructions": "Clean up.", "paths": ["old"]}, {"title": "Notes", "instructions": "Write notes.", "paths": ["notes.md"]}]}`)
	s.scripts[1] = []string{commandStep("rm -rf old"), writeStep("first.md", "one\n")}
	var sawFirst bool
	s.during[2] = func(dir string) {
		_, err := os.Stat(filepath.Join(dir, "first.md"))
		s.mu.Lock()
		sawFirst = err == nil
		s.mu.Unlock()
	}
	s.scripts[2] = []string{writeStep("notes.md", "two\n")}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.approve(ctx)
	answer, sw := s.run(ctx, s.profile(ctx, tools.PolicyAllow, tools.PolicyAllow), swarm.ModeSplit, folder, "Clean up the old folder and write notes.")
	if s.folders[1] != folder || s.folders[2] != folder {
		t.Fatalf("agents worked in %v, not the folder", s.folders)
	}
	if !sawFirst || strings.Join(strings.Fields(fmt.Sprint(s.order)), ",") != "[1,2]" {
		t.Errorf("the agents didn't take turns: order %v, agent 2 saw agent 1's file %v", s.order, sawFirst)
	}
	if read(t, folder, "notes.md") != "two\n" {
		t.Error("agent 2's file is missing")
	}
	if sw.Note != swarm.Reason(swarm.NotGit) || sw.Branch != "" || len(sw.Agents) != 2 {
		t.Fatalf("swarm = %+v", sw)
	}
	if !strings.Contains(answer, "Merged.") {
		t.Errorf("answer = %q", answer)
	}
	msgs, _ := s.Conversations.ListMessages(ctx, sw.ConversationID)
	if last := msgs[len(msgs)-1]; last.Meta == nil || last.Meta.Notice != swarm.Reason(swarm.NotGit) {
		t.Errorf("no notice under the answer: %+v", last.Meta)
	}
	if got := s.questions("filesystem.write"); len(got) != 2 || got[0]["swarm_id"] != sw.ID {
		t.Errorf("writes in a task didn't ask: %+v", got)
	}
	rm := s.questions("terminal")
	if len(rm) != 1 || rm[0]["always_asks"] != true || rm[0]["agent"] != "Agent 1: Clean" {
		t.Errorf("a destructive command didn't ask: %+v", rm)
	}
}

// A race on a task: each agent does the whole task in its own worktree, and
// the one whose checks fail can't win over the one whose checks pass.
func TestTaskSwarmRace(t *testing.T) {
	folder := repo(t, map[string]string{"go.mod": goMod, "greet.go": "package greet\n"})
	s := newTaskSwarm(t, "")
	failing := "package greet\n\nimport \"testing\"\n\nfunc TestHello(t *testing.T) {\n\tif Hello() != \"hello\" {\n\t\tt.Fatal(\"wrong\")\n\t}\n}\n"
	s.scripts[1] = []string{writeStep("hello.go", "package greet\n\nfunc Hello() string { return \"bye\" }\n"), writeStep("hello_test.go", failing)}
	s.scripts[2] = []string{writeStep("hello.go", "package greet\n\nfunc Hello() string { return \"hello\" }\n"), writeStep("hello_test.go", failing)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.approve(ctx)
	ctx2 := swarm.WithPolicy(ctx, &contracts.SwarmPolicy{Models: []string{models.StubModelID, models.StubModelID}})
	answer, sw := s.run(ctx2, s.profile(ctx, tools.PolicyAsk, tools.PolicyAllow), swarm.ModeRace, folder, "Add a Hello function that returns hello, with a test.")
	if sw.Winner != 2 || sw.Agents[0].Checks != swarm.ChecksFailed || sw.Agents[1].Checks != swarm.ChecksPassed || sw.Checks != swarm.ChecksPassed {
		t.Fatalf("swarm = %+v", sw)
	}
	if sw.Branch != swarm.BranchPrefix(sw.ID)+"agent-2" || !sw.Applied {
		t.Fatalf("branch %q, applied %v", sw.Branch, sw.Applied)
	}
	if !strings.Contains(read(t, folder, "hello.go"), `"hello"`) {
		t.Error("the losing change was applied")
	}
	if !strings.Contains(answer, "Agent 2's change was kept, and its checks passed") {
		t.Errorf("answer = %q", answer)
	}
}
