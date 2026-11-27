package simple

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/structured"
	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

func runWith(t *testing.T, env *planEnv, prompt string, o contracts.OrchestrationPolicy) string {
	t.Helper()
	events, err := New().Run(context.Background(), contracts.Task{Prompt: prompt}, contracts.AIProfile{
		Roles: []contracts.ModelRole{{Role: "assistant", ModelID: "m"}},
		Tools: []contracts.ToolPolicy{
			{ToolID: "internet.search", Policy: "allow"},
			{ToolID: "internet.open", Policy: "allow"},
		},
		Orchestration: o,
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

// A profile's orchestration controls change how a request is worked
// through (§40).
func TestProfileOrchestrationControls(t *testing.T) {
	const compare = "Compare Ollama, llama.cpp, MLX and vLLM for running models"

	env := &planEnv{}
	runWith(t, env, compare, contracts.OrchestrationPolicy{MaxWorkers: 2, Parallel: "off"})
	p := env.payload(EventPlanCreated)
	if p == nil || len(p["steps"].([]string)) != 2 || p["parallel"] != false {
		t.Fatalf("plan = %+v", p)
	}

	env = &planEnv{}
	runWith(t, env, compare, contracts.OrchestrationPolicy{Planning: "off"})
	if env.payload(EventPlanCreated) != nil {
		t.Fatal("planned with planning off")
	}

	// With figures that do not match the material, a check normally runs;
	// verification off skips it.
	const prompt = "How many tires are in stock?"
	env = &planEnv{reference: "Inventory: 12 tires in stock.", replies: []string{"There are 18 tires in stock."}}
	runWith(t, env, prompt, contracts.OrchestrationPolicy{})
	if !slices.Contains(env.events, EventVerified) {
		t.Fatalf("default did not check: %v", env.events)
	}
	env = &planEnv{reference: "Inventory: 12 tires in stock.", replies: []string{"There are 18 tires in stock."}}
	text := runWith(t, env, prompt, contracts.OrchestrationPolicy{Verification: "off"})
	if slices.Contains(env.events, EventVerified) || slices.Contains(env.events, EventVerifying) || text != "There are 18 tires in stock." {
		t.Fatalf("verification off: events=%v text=%q", env.events, text)
	}
}

// A turn whose answer must be JSON makes one reply and returns it as is,
// even when it looks like a tool call, after looking up the web (§27).
func TestJSONOnlyTurn(t *testing.T) {
	env := &planEnv{replies: []string{`{"name": "internet.search", "moons": 146}`}}
	ctx := structured.WithSchema(context.Background(), []byte(`{"type":"object"}`))
	events, err := New().Run(ctx, contracts.Task{Prompt: "Compare Saturn, Jupiter and Mars moons today"}, contracts.AIProfile{
		Roles: []contracts.ModelRole{{Role: "assistant", ModelID: "m"}},
		Tools: []contracts.ToolPolicy{{ToolID: "internet.search", Policy: "allow"}, {ToolID: "internet.open", Policy: "allow"}},
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
	if text != `{"name": "internet.search", "moons": 146}` || len(env.seen) != 1 {
		t.Fatalf("text=%q generations=%d", text, len(env.seen))
	}
	if env.payload(EventPlanCreated) != nil {
		t.Fatal("planned a JSON-only turn")
	}
	if sys := env.seen[0][0].Content; strings.Contains(sys, "tool_call") {
		t.Fatalf("tools offered: %q", sys)
	}
}

// A turn that answers without thinking, such as a spoken one, revises its
// answer without thinking too: a revision that thought held a voice answer
// back for a minute and a half.
func TestNoThinkingRevision(t *testing.T) {
	const prompt = "How many tires are in stock?"
	env := &planEnv{reference: "Inventory: 12 tires in stock.", replies: []string{"There are 18 tires in stock.", "There are 12 tires in stock."}}
	ctx := pluginapi.WithAnswerOptions(context.Background(), pluginapi.GenerateOptions{NoThinking: true})
	events, err := New().Run(ctx, contracts.Task{Prompt: prompt}, contracts.AIProfile{
		Roles: []contracts.ModelRole{{Role: "assistant", ModelID: "m"}},
	}, env)
	if err != nil {
		t.Fatal(err)
	}
	for range events {
	}
	if !slices.Contains(env.events, EventVerifying) || len(env.opts) < 2 {
		t.Fatalf("no revision: events=%v calls=%d", env.events, len(env.opts))
	}
	for i, o := range env.opts {
		if !o.NoThinking {
			t.Errorf("call %d thought: %+v", i, o)
		}
	}
}

// An answer that sends the person to look, though the web was looked up
// first, is looked up again reading more, and written from what was found.
func TestDeflectionAfterLookupLooksDeeper(t *testing.T) {
	env := &planEnv{replies: []string{"You could check the city's events calendar website.", "The Winter Folk Festival is at Centennial Hall tonight."}}
	text := runWith(t, env, "What events are going on around town today?", contracts.OrchestrationPolicy{})
	if len(env.searches) != 2 || !strings.Contains(text, "Folk Festival") {
		t.Fatalf("searches=%q text=%q", env.searches, text)
	}
}

// An answer that doesn't know is looked up, and written from what the web
// says; one that only asks the person back isn't.
func TestNotKnowingIsLookedUp(t *testing.T) {
	env := &planEnv{replies: []string{"I don't know who wrote that book.", "It was written by Jane Doe."}}
	text := runWith(t, env, "Who wrote the book Sea Ice Dreams?", contracts.OrchestrationPolicy{})
	if len(env.searches) != 1 || !strings.Contains(text, "Jane Doe") {
		t.Fatalf("searches=%q text=%q", env.searches, text)
	}
	// It reads the top pages, and says it looked it up.
	last := env.seen[len(env.seen)-1]
	if !strings.Contains(last[0].Content, lookedAgainGuidance) || strings.Contains(last[0].Content, "Do not mention the search") || !strings.Contains(last[len(last)-1].Content, "Page read:") {
		t.Fatalf("rewrite: system=%q user=%q", last[0].Content, last[len(last)-1].Content)
	}
	env = &planEnv{replies: []string{"Which book do you mean? Could you tell me the author?"}}
	runWith(t, env, "Who wrote that book I mentioned?", contracts.OrchestrationPolicy{})
	if len(env.searches) != 0 {
		t.Fatalf("looked up a question back: %q", env.searches)
	}
}

// Spoken, the rewrite doesn't say it looked it up: voice mode already did
// as the search started (#1487).
func TestNotKnowingSpokenLeavesTheFillerToVoice(t *testing.T) {
	env := &planEnv{replies: []string{"I don't know who wrote that book.", "It was written by Jane Doe."}}
	ctx := turnopts.WithSpoken(context.Background())
	events, err := New().Run(ctx, contracts.Task{Prompt: "Who wrote the book Sea Ice Dreams?"}, contracts.AIProfile{
		Roles: []contracts.ModelRole{{Role: "assistant", ModelID: "m"}},
		Tools: []contracts.ToolPolicy{{ToolID: "internet.search", Policy: "allow"}, {ToolID: "internet.open", Policy: "allow"}},
	}, env)
	if err != nil {
		t.Fatal(err)
	}
	for range events {
	}
	sys := env.seen[len(env.seen)-1][0].Content
	if !strings.Contains(sys, lookedAgainSpoken) || strings.Contains(sys, lookedAgainGuidance) {
		t.Fatalf("spoken rewrite: %q", sys)
	}
}
