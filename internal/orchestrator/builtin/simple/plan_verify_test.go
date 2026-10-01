package simple

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/yeixio/yggdrasil-core/internal/huginn"
	"github.com/yeixio/yggdrasil-core/pkg/contracts"
	"github.com/yeixio/yggdrasil-core/pkg/pluginapi"
)

// planEnv answers in order, records events, and is safe for the parallel
// lookups a plan makes.
type planEnv struct {
	mu        sync.Mutex
	replies   []string
	n         int
	seen      [][]pluginapi.ChatMessage
	searches  []string
	events    []string
	payloads  []map[string]any
	reference string
}

func (e *planEnv) Generate(ctx context.Context, role string, messages []pluginapi.ChatMessage) (<-chan pluginapi.ChatChunk, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	content := "done"
	if e.n < len(e.replies) {
		content = e.replies[e.n]
		e.n++
	}
	e.seen = append(e.seen, append([]pluginapi.ChatMessage(nil), messages...))
	ch := make(chan pluginapi.ChatChunk, 1)
	ch <- pluginapi.ChatChunk{Content: content, Done: true}
	close(ch)
	return ch, nil
}

func (e *planEnv) ExecuteTool(ctx context.Context, toolID string, args map[string]any) (map[string]any, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if toolID == "internet.search" {
		q, _ := args["query"].(string)
		e.searches = append(e.searches, q)
		return map[string]any{"results": []any{map[string]any{"title": q + " home", "url": "https://example.com/" + strings.ReplaceAll(q, " ", "-"), "snippet": q + " facts"}}}, nil
	}
	return map[string]any{"title": "page", "url": args["url"], "content": "Plenty of readable page text about the subject for the notes. It is long enough to count as a useful page for this test."}, nil
}

func (e *planEnv) Emit(eventType string, payload map[string]any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, eventType)
	e.payloads = append(e.payloads, payload)
}

func (e *planEnv) NodeForRole(role string) (string, error) { return "local", nil }

func (e *planEnv) ReferenceMaterial(ctx context.Context, prompt string) string { return e.reference }

func (e *planEnv) payload(event string) map[string]any {
	for i, ev := range e.events {
		if ev == event {
			return e.payloads[i]
		}
	}
	return nil
}

func run(t *testing.T, env *planEnv, prompt string, tools ...contracts.ToolPolicy) string {
	t.Helper()
	return runAt(t, context.Background(), env, prompt, tools...)
}

func runAt(t *testing.T, ctx context.Context, env *planEnv, prompt string, tools ...contracts.ToolPolicy) string {
	t.Helper()
	events, err := New().Run(ctx, contracts.Task{Prompt: prompt}, contracts.AIProfile{
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

func TestComparisonIsResearchedInParts(t *testing.T) {
	env := &planEnv{replies: []string{
		"Ollama: easy installer, uses llama.cpp underneath.",
		"MLX: Apple's array framework, fast on Apple Silicon.",
		"Ollama is easiest to start with; MLX is fastest on a Mac.",
	}}
	text := run(t, env, "Research Ollama and MLX for running models on a Mac",
		contracts.ToolPolicy{ToolID: "internet.search", Policy: "allow"},
		contracts.ToolPolicy{ToolID: "internet.open", Policy: "allow"})

	if len(env.searches) != 2 {
		t.Fatalf("one lookup per part, got %v", env.searches)
	}
	if p := env.payload(EventPlanCreated); p == nil || p["parallel"] != true {
		t.Fatalf("plan event = %+v", p)
	}
	if len(env.seen) != 3 {
		t.Fatalf("two parts and a final answer, got %d generations", len(env.seen))
	}
	for _, part := range env.seen[:2] {
		if !strings.Contains(part[0].Content, "one part of a larger request") || !strings.Contains(part[1].Content, "Your part: Find out about") {
			t.Fatalf("part prompt = %+v", part)
		}
	}
	final := env.seen[2]
	sys, user := final[0].Content, final[len(final)-1].Content
	if !strings.Contains(sys, "worked through this request in parts") || strings.Contains(sys, "internet.search") {
		t.Fatalf("final system = %q", sys)
	}
	if !strings.Contains(user, "[1] Find out about Ollama") || !strings.Contains(user, "Apple's array framework") {
		t.Fatalf("final user = %q", user)
	}
	if text != "Ollama is easiest to start with; MLX is fastest on a Mac." {
		t.Fatalf("answer = %q", text)
	}
}

func TestSequenceBuildsOnEarlierParts(t *testing.T) {
	env := &planEnv{replies: []string{"Notes on part one.", "Notes on part two.", "Final."}}
	run(t, env, "Outline a garden plan, then list the plants for each bed, then write a watering schedule")
	if len(env.seen) != 4 || !strings.Contains(env.seen[1][1].Content, "Notes on part one.") {
		t.Fatalf("the second part must see the first part's notes: %+v", env.seen)
	}
}

func TestAnswerFiguresAreChecked(t *testing.T) {
	env := &planEnv{
		reference: "item: Michelin Defender; price: 176.50; in stock: 4",
		replies: []string{
			"The Michelin Defender costs $176.50 and 20 are in stock.",
			"The Michelin Defender costs $176.50 and 4 are in stock.",
		},
	}
	text := run(t, env, "How much is the Michelin Defender and is it in stock?")
	if text != "The Michelin Defender costs $176.50 and 4 are in stock." {
		t.Fatalf("answer = %q", text)
	}
	if !strings.Contains(env.seen[1][len(env.seen[1])-1].Content, "20 does not appear in the reference material") {
		t.Fatal("the correction must name the figure")
	}
	if p := env.payload(EventVerified); p == nil || p["fixed"] != 1 || p["remaining"] != "" {
		t.Fatalf("verified = %+v", p)
	}
}

func TestUncorrectedFiguresAreReported(t *testing.T) {
	env := &planEnv{
		reference: "item: Michelin Defender; price: 176.50; in stock: 4",
		replies:   []string{"It costs $176.50 and 20 are in stock.", "It costs $176.50 and 20 are in stock, really."},
	}
	text := run(t, env, "How much is the Michelin Defender?")
	if text != "It costs $176.50 and 20 are in stock." {
		t.Fatalf("answer = %q", text)
	}
	if p := env.payload(EventVerified); p == nil || p["remaining"] != "20" {
		t.Fatalf("verified = %+v", p)
	}
}

func TestPlainAnswersAreNotChecked(t *testing.T) {
	env := &planEnv{replies: []string{"DNS turns names into addresses."}}
	run(t, env, "What is DNS?")
	if len(env.seen) != 1 || len(env.events) != 1 || env.events[0] != EventEffort {
		t.Fatalf("a plain answer needs no plan and no check: %d generations, events %v", len(env.seen), env.events)
	}
}

func TestAnswerThatDescribesToolsIsRetriedWithoutTools(t *testing.T) {
	env := &planEnv{
		reference: "item: Michelin Defender; price: 176.50; in stock: 4",
		replies: []string{
			"To find the price we can use internet.search and then filesystem.read the stock file.",
			"The Michelin Defender costs $176.50 and 4 are in stock.",
		},
	}
	text := run(t, env, "How much is the Michelin Defender?",
		contracts.ToolPolicy{ToolID: "internet.search", Policy: "ask"},
		contracts.ToolPolicy{ToolID: "filesystem.read", Policy: "allow"})
	if text != "The Michelin Defender costs $176.50 and 4 are in stock." {
		t.Fatalf("answer = %q", text)
	}
	if strings.Contains(env.seen[1][0].Content, "internet.search") {
		t.Fatal("the retry must not offer tools")
	}
}

func TestFastSkipsPlansAndCorrections(t *testing.T) {
	fast := huginn.WithEffort(context.Background(), huginn.EffortFast)
	env := &planEnv{replies: []string{"Ollama is easiest; MLX is fastest."}}
	runAt(t, fast, env, "Research Ollama and MLX for running models on a Mac")
	if len(env.seen) != 1 || env.payload(EventPlanCreated) != nil {
		t.Fatalf("Fast answers in one go: %d generations", len(env.seen))
	}
	if p := env.payload(EventEffort); p["effort"] != "fast" || p["chosen"] != "fast" {
		t.Fatalf("effort = %+v", p)
	}

	env = &planEnv{
		reference: "item: Michelin Defender; price: 176.50; in stock: 4",
		replies:   []string{"It costs $176.50 and 20 are in stock.", "unused"},
	}
	text := runAt(t, fast, env, "How much is the Michelin Defender?")
	if text != "It costs $176.50 and 20 are in stock." || len(env.seen) != 1 {
		t.Fatalf("Fast reports figures without a correction pass: %q, %d generations", text, len(env.seen))
	}
	if p := env.payload(EventVerified); p == nil || p["remaining"] != "20" {
		t.Fatalf("verified = %+v", p)
	}
}

func TestAutoResolvesEffortFromTheRequest(t *testing.T) {
	env := &planEnv{replies: []string{"DNS turns names into addresses."}}
	run(t, env, "What is DNS?")
	if p := env.payload(EventEffort); p["effort"] != "fast" || p["chosen"] != "auto" {
		t.Fatalf("a quick question = %+v", p)
	}
	env = &planEnv{reference: "item: Tire; price: 10", replies: []string{"It is $10."}}
	run(t, env, "How much is the tire?")
	if p := env.payload(EventEffort); p["effort"] != "thorough" {
		t.Fatalf("a question about the user's data = %+v", p)
	}
}
