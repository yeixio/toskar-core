package simple

import (
	"context"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

func repairRun(t *testing.T, env *scriptedEnv, prompt string, tools ...contracts.ToolPolicy) string {
	t.Helper()
	events, err := New().Run(context.Background(), contracts.Task{Prompt: prompt}, contracts.AIProfile{
		Roles: []contracts.ModelRole{{Role: "assistant", ModelID: "m"}},
		Tools: tools,
	}, env)
	if err != nil {
		t.Fatal(err)
	}
	var text string
	for evt := range events {
		if evt.Type == "agent.message" {
			text += evt.Content
		}
	}
	return text
}

// A call numbered as OpenAI's are, that only one offered tool fits, runs
// as that tool (#1213).
func TestNumberedCallRunsTheOneToolItFits(t *testing.T) {
	env := &scriptedEnv{replies: []string{
		`{"tool_call":{"id":"1","args":{"query":"Juneau weather"}}}`,
		"It is 48 F and cloudy in Juneau.",
	}}
	text := repairRun(t, env, "weather", contracts.ToolPolicy{ToolID: "internet.search", Policy: "ask"})
	if env.tools != 1 || !strings.Contains(text, "48 F") {
		t.Fatalf("tools=%d text=%q", env.tools, text)
	}
}

// When two tools fit, the model is shown its call with each, to pick one.
func TestNumberedCallNoteNamesTheToolsThatFit(t *testing.T) {
	profile := contracts.AIProfile{Tools: []contracts.ToolPolicy{
		{ToolID: "internet.search", Policy: "allow"},
		{ToolID: "filesystem.search", Policy: "allow"},
		{ToolID: "filesystem.read", Policy: "allow"},
	}}
	content := `{"tool_call":{"id":"search1","args":{"query":"coffee near the harbor"}}}`
	if _, ok := repairedCall(content, profile); ok {
		t.Fatal("guessed between two tools")
	}
	note, ok := numberedCallNote(content, profile)
	if !ok {
		t.Fatal("no note")
	}
	for _, want := range []string{`"search1" is not a tool`, `{"tool_call":{"id":"internet.search","args":{"query":"coffee near the harbor"}}}`, `"id":"filesystem.search"`} {
		if !strings.Contains(note, want) {
			t.Errorf("note lacks %s: %s", want, note)
		}
	}
	if strings.Contains(note, "filesystem.read") {
		t.Errorf("a tool that doesn't fit was offered: %s", note)
	}
	// A call naming a real tool isn't numbered.
	if _, _, ok := numberedCall(`{"tool_call":{"id":"internet.search","args":{"query":"x"}}}`); ok {
		t.Fatal("a named call read as numbered")
	}
}

// A model that never makes a valid call is asked once for a plain answer,
// instead of the turn ending with none.
func TestOnlyInvalidCallsStillAnswer(t *testing.T) {
	bad := `{"tool_call":{"id":"7","args":{"where":"harbor"}}}`
	env := &scriptedEnv{replies: []string{bad, bad, bad, "I can't look it up now, but Juneau's harbor has a few cafés."}}
	text := repairRun(t, env, "weather", contracts.ToolPolicy{ToolID: "internet.search", Policy: "ask"})
	if env.tools != 0 || !strings.Contains(text, "cafés") {
		t.Fatalf("tools=%d text=%q", env.tools, text)
	}
	last := env.seen[len(env.seen)-1]
	if !strings.Contains(last[len(last)-1].Content, "No tool can run for this now") {
		t.Fatalf("not asked for a plain answer: %q", last[len(last)-1].Content)
	}
}
