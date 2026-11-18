package simple

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// planningEnv is a reasoning model that thinks before each step.
type planningEnv struct{ scriptedEnv }

func (e *planningEnv) Generate(ctx context.Context, role string, messages []pluginapi.ChatMessage) (<-chan pluginapi.ChatChunk, error) {
	step := e.n
	inner, _ := e.scriptedEnv.Generate(ctx, role, messages)
	ch := make(chan pluginapi.ChatChunk, 3)
	ch <- pluginapi.ChatChunk{Reasoning: "Plan for step " + string(rune('1'+step)) + ": search first, then answer."}
	for c := range inner {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func runPlanning(t *testing.T, mode string) *planningEnv {
	t.Helper()
	old := reasoningMode
	reasoningMode = func() string { return mode }
	t.Cleanup(func() { reasoningMode = old })
	env := &planningEnv{scriptedEnv{replies: []string{
		`{"tool_call":{"id":"internet.search","args":{"query":"Juneau weather"}}}`,
		"It is 48 F and cloudy in Juneau.",
	}}}
	events, err := New().Run(context.Background(), contracts.Task{Prompt: "weather"}, contracts.AIProfile{
		Roles: []contracts.ModelRole{{Role: "assistant", ModelID: "m"}},
		Tools: []contracts.ToolPolicy{{ToolID: "internet.search", Policy: "ask"}},
	}, env)
	if err != nil {
		t.Fatal(err)
	}
	for range events {
	}
	if len(env.seen) < 2 {
		t.Fatalf("asked %d times", len(env.seen))
	}
	return env
}

// stepMessage is the assistant message the second request carries for the
// tool step.
func stepMessage(t *testing.T, env *planningEnv) pluginapi.ChatMessage {
	t.Helper()
	for _, m := range env.seen[1] {
		if m.Role == "assistant" && strings.Contains(m.Content, "tool_call") {
			return m
		}
	}
	t.Fatalf("no tool step in %+v", env.seen[1])
	return pluginapi.ChatMessage{}
}

// Within one turn, a reasoning model's thinking before a tool call goes
// back with that step, as the mode says; off, it doesn't (#841).
func TestReasoningKeptBetweenToolCalls(t *testing.T) {
	off := stepMessage(t, runPlanning(t, reasoningOff))
	if off.Reasoning != "" || strings.Contains(off.Content, "Plan for step") {
		t.Fatalf("off kept the thinking: %+v", off)
	}

	field := stepMessage(t, runPlanning(t, reasoningField))
	if field.Reasoning != "Plan for step 1: search first, then answer." || strings.Contains(field.Content, "Plan for step") {
		t.Fatalf("field = %+v", field)
	}
	b, _ := json.Marshal(field)
	if !strings.Contains(string(b), `"reasoning_content":"Plan for step 1`) {
		t.Fatalf("sent as %s", b)
	}

	inline := stepMessage(t, runPlanning(t, reasoningInline))
	if inline.Reasoning != "" || !strings.HasPrefix(inline.Content, "My reasoning before this step:\nPlan for step 1") || !strings.Contains(inline.Content, "tool_call") {
		t.Fatalf("inline = %+v", inline)
	}
}

// Only the end of a long thought goes back, where the next step's plan is.
func TestKeptReasoningIsBounded(t *testing.T) {
	old := reasoningMode
	reasoningMode = func() string { return reasoningField }
	defer func() { reasoningMode = old }()
	long := strings.Repeat("a", maxKeptReasoning) + "the plan"
	m := keptReasoning(pluginapi.ChatMessage{Role: "assistant", Content: "x"}, long)
	if r := []rune(m.Reasoning); len(r) != maxKeptReasoning+1 || !strings.HasSuffix(m.Reasoning, "the plan") || !strings.HasPrefix(m.Reasoning, "…") {
		t.Fatalf("kept %d runes", len(r))
	}
	if keptReasoning(pluginapi.ChatMessage{Content: "x"}, "  ").Reasoning != "" {
		t.Fatal("empty thinking kept")
	}
}

// The mode is read from TOSKAR_KEEP_REASONING.
func TestReasoningModeFromEnvironment(t *testing.T) {
	for v, want := range map[string]string{"": reasoningOff, "field": reasoningField, "INLINE": reasoningInline, "1": reasoningField, "nope": reasoningOff} {
		t.Setenv("TOSKAR_KEEP_REASONING", v)
		if got := reasoningMode(); got != want {
			t.Errorf("%q = %q, want %q", v, got, want)
		}
	}
}
