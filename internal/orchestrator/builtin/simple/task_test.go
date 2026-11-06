package simple

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/runlog"
	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

type taskEnv struct {
	*roleEnv
	ran []string
}

func (e *taskEnv) ExecuteTool(_ context.Context, _ string, args map[string]any) (map[string]any, error) {
	c, _ := args["command"].(string)
	e.ran = append(e.ran, c)
	return map[string]any{"stdout": "out", "exit": 0}, nil
}

func call(command string) string {
	return fmt.Sprintf(`{"tool_call":{"id":"terminal","args":{"command":%q}}}`, command)
}

// exitEnv runs every command, and each one fails.
type exitEnv struct{ *roleEnv }

func (e *exitEnv) ExecuteTool(context.Context, string, map[string]any) (map[string]any, error) {
	return map[string]any{"stdout": "FAIL", "exit": 1}, nil
}

func runTaskWith(t *testing.T, env *exitEnv, prompt string, profile contracts.AIProfile, replies ...string) (*exitEnv, string, []string) {
	t.Helper()
	inner := newRoleEnv(map[string]string{"assistant": replies[0]})
	inner.sequence["assistant"] = replies[1:]
	env.roleEnv = inner
	ctx := runlog.WithOrigin(context.Background(), runlog.Origin{Client: "terminal"})
	events, err := New().Run(ctx, contracts.Task{Prompt: prompt}, profile, env)
	if err != nil {
		t.Fatal(err)
	}
	answer := ""
	for e := range events {
		if e.Type == "agent.message" {
			answer += e.Content
		}
	}
	inner.mu.Lock()
	defer inner.mu.Unlock()
	return env, answer, inner.prompts["assistant"]
}

func runTask(t *testing.T, client, prompt string, profile contracts.AIProfile, replies ...string) (*taskEnv, string, []string) {
	t.Helper()
	inner := newRoleEnv(map[string]string{"assistant": replies[0]})
	inner.sequence["assistant"] = replies[1:]
	env := &taskEnv{roleEnv: inner}
	ctx := runlog.WithOrigin(context.Background(), runlog.Origin{Client: client})
	events, err := New().Run(ctx, contracts.Task{Prompt: prompt}, profile, env)
	if err != nil {
		t.Fatal(err)
	}
	answer := ""
	for e := range events {
		if e.Type == "agent.message" {
			answer += e.Content
		}
	}
	inner.mu.Lock()
	defer inner.mu.Unlock()
	return env, answer, inner.prompts["assistant"]
}

func taskProfile() contracts.AIProfile {
	return contracts.AIProfile{
		Roles: []contracts.ModelRole{{Role: "assistant", ModelID: "m", Required: true}},
		Tools: []contracts.ToolPolicy{{ToolID: "terminal", Policy: "allow"}, {ToolID: "git.status", Policy: "allow"}, {ToolID: "filesystem.read", Policy: "allow"}},
	}
}

// Work asked for in the terminal goes on past a question's few steps, and ends
// when the model says it's done (#1017).
func TestTaskKeepsGoing(t *testing.T) {
	var replies []string
	for i := range 20 {
		replies = append(replies, call(fmt.Sprintf("make step%d", i)))
	}
	replies = append(replies, "Done: all 20 steps ran.")
	env, answer, _ := runTask(t, "terminal", "Fix the failing test", taskProfile(), replies...)
	if len(env.ran) != 20 || !strings.Contains(answer, "all 20 steps") {
		t.Fatalf("ran %d, answered %q", len(env.ran), answer)
	}
}

// The same work asked from elsewhere, or a question, keeps its usual budget.
func TestTaskIsOnlyWorkFromTheTerminal(t *testing.T) {
	var replies []string
	for i := range 30 {
		replies = append(replies, call(fmt.Sprintf("make step%d", i)))
	}
	replies = append(replies, "Done.")
	if env, _, _ := runTask(t, "web", "Fix the failing test", taskProfile(), replies...); len(env.ran) > 16 {
		t.Errorf("web ran %d", len(env.ran))
	}
	if env, _, _ := runTask(t, "terminal", "What's a good first car?", taskProfile(), replies...); len(env.ran) > 3 {
		t.Errorf("a question ran %d", len(env.ran))
	}
	// A cap the profile sets holds.
	capped := taskProfile()
	capped.Orchestration.MaxToolCalls = 2
	if env, _, _ := runTask(t, "terminal", "Fix the failing test", capped, replies...); len(env.ran) != 2 {
		t.Errorf("capped ran %d", len(env.ran))
	}
}

// A task going round in circles stops, and says what it did and what's left.
func TestTaskStopsWhenStuck(t *testing.T) {
	env, answer, prompts := runTask(t, "terminal", "Fix the failing test", taskProfile(),
		call("make test"), call("make test"), call("make test"),
		"I ran the tests three times. They still fail; the fix is left to do.")
	if len(env.ran) != 3 {
		t.Fatalf("ran %v", env.ran)
	}
	if !strings.Contains(answer, "left to do") {
		t.Fatalf("answer %q", answer)
	}
	if last := prompts[len(prompts)-1]; !strings.Contains(last, "keeps giving the same result") {
		t.Fatalf("last prompt %q", last)
	}
}

// A model that won't stop and sum up still leaves an answer, saying what ran.
func TestTaskReportsWhenTheModelWont(t *testing.T) {
	env, answer, _ := runTask(t, "terminal", "Fix the failing test", taskProfile(),
		call("make test"), call("make test"), call("make test"), call("make test"), call("make test"))
	if len(env.ran) != 3 || !strings.Contains(answer, "keeps giving the same result") || !strings.Contains(answer, "- make test") {
		t.Fatalf("ran %v, answered %q", env.ran, answer)
	}
}

// A reply with nothing in it, such as an empty tool call, doesn't end a task.
func TestTaskGoesOnAfterAnEmptyReply(t *testing.T) {
	env, answer, _ := runTask(t, "terminal", "Fix the failing test", taskProfile(),
		call("go test"), "```tool_call\n\n```", call("go test ./x"), "The tests pass now.")
	if len(env.ran) != 2 || !strings.Contains(answer, "pass now") {
		t.Fatalf("ran %v, answered %q", env.ran, answer)
	}
	// Twice, it's asked to sum up; with nothing still, Toskar says what ran.
	env, answer, _ = runTask(t, "terminal", "Fix the failing test", taskProfile(),
		call("go test"), "```tool_call\n\n```", "```tool_call\n\n```", "```tool_call\n\n```", "```tool_call\n\n```")
	if len(env.ran) != 1 || !strings.Contains(answer, "without saying where things stand") || !strings.Contains(answer, "- go test") {
		t.Fatalf("ran %v, answered %q", env.ran, answer)
	}
}

// Saying what's next and stopping isn't finishing.
func TestTaskGoesOnAfterSayingWhatsNext(t *testing.T) {
	env, answer, _ := runTask(t, "terminal", "Fix the failing test", taskProfile(),
		call("go test"), "The code is wrong.\n\nI need to fix the sum.go file.", call("make fix"), "Fixed. The tests pass.")
	if len(env.ran) != 2 || !strings.Contains(answer, "tests pass") {
		t.Fatalf("ran %v, answered %q", env.ran, answer)
	}
	// It doesn't go on forever.
	env, answer, _ = runTask(t, "terminal", "Fix the failing test", taskProfile(),
		call("go test"), "I will fix it.", "I will fix it.", "I will fix it.", "I will fix it.", "I will fix it.")
	if len(env.ran) != 1 || !strings.Contains(answer, "I will fix it") {
		t.Fatalf("ran %v, answered %q", env.ran, answer)
	}
	for text, want := range map[string]bool{
		"I'll run the tests.":                true,
		"First I fixed it. Now let's test.":  true,
		"I fixed it. The tests pass.":        false,
		"I will fix it. Then it passed.":     false,
		"Done: what's left is nothing.":      false,
		"I updated the file, and it passes.": false,
	} {
		if got := soundsUnfinished(text); got != want {
			t.Errorf("%q: %v", text, got)
		}
	}
}

// At the limit it stops the same way, not with a stock sentence.
func TestTaskStopsAtTheLimit(t *testing.T) {
	var replies []string
	for i := range 101 {
		replies = append(replies, call(fmt.Sprintf("make step%d", i)))
	}
	replies = append(replies, "I got through 100 steps; the rest is left to do.")
	env, answer, _ := runTask(t, "terminal", "Fix the failing test", taskProfile(), replies...)
	if len(env.ran) != 100 || !strings.Contains(answer, "left to do") {
		t.Fatalf("ran %d, answered %q", len(env.ran), answer)
	}
}

func TestFoldOlderSteps(t *testing.T) {
	msgs := []pluginapi.ChatMessage{{Role: "system", Content: "s"}, {Role: "user", Content: "ask"}}
	start := len(msgs)
	for i := range 30 {
		msgs = append(msgs, pluginapi.ChatMessage{Role: "assistant", Content: fmt.Sprintf("step %d", i)}, pluginapi.ChatMessage{Role: "user", Content: "result"})
		msgs = foldOlderSteps(msgs, start)
	}
	if len(msgs) > start+2+keptSteps*2 {
		t.Fatalf("%d messages", len(msgs))
	}
	if msgs[1].Content != "ask" || !strings.Contains(msgs[start].Content, "- step 0: ok") || !strings.Contains(msgs[start].Content, "- step 21: ok") {
		t.Fatalf("%q", msgs[start].Content)
	}
	if strings.Contains(msgs[start].Content, "- step 22:") || msgs[len(msgs)-2].Content != "step 29" {
		t.Fatalf("%+v", msgs)
	}
}

func TestShortenResultKeepsTheEnd(t *testing.T) {
	long := "start" + strings.Repeat("x", 50000) + "the error"
	got := shortenResult(long)
	if len(got) > maxResult+40 || !strings.HasPrefix(got, "start") || !strings.HasSuffix(got, "the error") {
		t.Fatalf("%d", len(got))
	}
}

// A task knows its folder and follows the project's notes, which can't change
// what needs approval (#1017).
func TestTaskGuidanceHasTheProject(t *testing.T) {
	if got := taskGuidance(context.Background()); strings.Contains(got, "project folder") {
		t.Fatalf("no project: %q", got)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("second choice"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("Use make test."), 0o600); err != nil {
		t.Fatal(err)
	}
	got := taskGuidance(turnopts.WithProject(context.Background(), dir))
	for _, want := range []string{dir, "Use make test.", "from AGENTS.md", "don't change what you're allowed"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in %q", want, got)
		}
	}
	if strings.Contains(got, "second choice") {
		t.Errorf("read both: %q", got)
	}
}

// A question that uses all its tool calls is answered from what they found,
// not left without an answer (#1101); one that still won't stops as before.
func TestQuestionAnswersAtItsLimit(t *testing.T) {
	env, answer, prompts := runTask(t, "terminal", "Which shell command lists hidden files?", taskProfile(),
		call("ls"), call("ls -a"), call("ls -la"), call("ls -R"), "ls -a lists them.")
	if len(env.ran) != 3 || answer != "ls -a lists them." {
		t.Fatalf("ran %v, answered %q", env.ran, answer)
	}
	if last := prompts[len(prompts)-1]; !strings.Contains(last, "Don't call any more tools") {
		t.Fatalf("last prompt %q", last)
	}
	_, answer, _ = runTask(t, "terminal", "Which shell command lists hidden files?", taskProfile(),
		call("ls"), call("ls -a"), call("ls -la"), call("ls -R"), call("ls -R /"))
	if !strings.Contains(answer, "stopped after 3 tool calls") {
		t.Fatalf("answered %q", answer)
	}
}
