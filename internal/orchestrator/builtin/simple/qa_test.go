package simple

// Regression tests for chat bugs the Nov 2026 QA pass filed (layer 6 of
// the test plan: privacy scenarios with a stub model and spy tools). While
// an issue is open, its test is wrapped in qa.KnownBug, which skips while
// the bug is there and fails once it's fixed.

import (
	"context"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/qa"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// knowledgeEnv answers from a connected notes folder and records every
// tool the turn calls, so a web search shows up.
type knowledgeEnv struct {
	scriptedEnv
	called []string
}

func (e *knowledgeEnv) ReferenceMaterial(context.Context, string) string {
	return "[1] notes/bakery.md\nQA Bakery: sourdough $7.25, closed Tuesdays."
}

func (e *knowledgeEnv) ExecuteTool(ctx context.Context, toolID string, args map[string]any) (map[string]any, error) {
	e.called = append(e.called, toolID)
	return e.scriptedEnv.ExecuteTool(ctx, toolID, args)
}

// A question that connected Knowledge answers doesn't also go to a web
// search, even with web search allowed without asking.
func TestQAKnowledgeAnswerStaysLocal(t *testing.T) {
	// Regression: toskar-core#1268
	env := &knowledgeEnv{scriptedEnv: scriptedEnv{replies: []string{
		"A sourdough loaf at the QA Bakery is $7.25, and it's closed on Tuesdays [1].",
	}}}
	events, err := New().Run(context.Background(), contracts.Task{Prompt: "How much is a sourdough loaf at the QA Bakery, and which day is it closed?"}, contracts.AIProfile{
		Roles: []contracts.ModelRole{{Role: "assistant", ModelID: "m"}},
		Tools: []contracts.ToolPolicy{
			{ToolID: "internet.search", Policy: "allow"},
			{ToolID: "internet.open", Policy: "allow"},
		},
	}, env)
	if err != nil {
		t.Fatal(err)
	}
	for range events {
	}
	var web []string
	for _, id := range env.called {
		if strings.HasPrefix(id, "internet.") {
			web = append(web, id)
		}
	}
	qa.KnownBug(t, "toskar-core#1268", func(t testing.TB) {
		if len(web) > 0 {
			t.Errorf("a question Knowledge answers went to the web too: %v", web)
		}
	})
}
