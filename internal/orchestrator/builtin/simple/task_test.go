package simple

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/runlog"
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
