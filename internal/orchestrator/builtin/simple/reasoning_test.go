package simple

import (
	"context"
	"testing"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// thinkingEnv streams a reasoning model's thinking, then its answer.
type thinkingEnv struct {
	scriptedEnv
	emitted []map[string]any
}

func (e *thinkingEnv) Generate(ctx context.Context, role string, messages []pluginapi.ChatMessage) (<-chan pluginapi.ChatChunk, error) {
	ch := make(chan pluginapi.ChatChunk, 4)
	ch <- pluginapi.ChatChunk{Reasoning: "17 × 23: 17 × 20 is 340, "}
	ch <- pluginapi.ChatChunk{Reasoning: "17 × 3 is 51, so 391."}
	ch <- pluginapi.ChatChunk{Content: "391", Done: true}
	close(ch)
	return ch, nil
}

func (e *thinkingEnv) Emit(eventType string, payload map[string]any) {
	if eventType == EventReasoning {
		e.emitted = append(e.emitted, payload)
	}
}

// A reasoning model's thinking says it's reasoning, then how long it took,
// and is never part of the answer (#701).
func TestReasoningIsShownNotAnswered(t *testing.T) {
	env := &thinkingEnv{}
	text, _, err := generate(context.Background(), env, "primary", nil, true)
	if err != nil || text != "391" {
		t.Fatalf("answer = %q, %v", text, err)
	}
	if len(env.emitted) != 2 || env.emitted[0]["state"] != "started" || env.emitted[1]["state"] != "done" {
		t.Fatalf("events = %v", env.emitted)
	}
	done := env.emitted[1]
	if done["answer"] != true || done["text"] != "17 × 23: 17 × 20 is 340, 17 × 3 is 51, so 391." {
		t.Fatalf("done = %v", done)
	}
	if _, ok := done["ms"].(int64); !ok {
		t.Fatalf("ms = %#v", done["ms"])
	}

	// A check or helper call says how long, but keeps no text.
	env.emitted = nil
	if _, _, err := generateText(context.Background(), env, "reviewer", nil); err != nil {
		t.Fatal(err)
	}
	if len(env.emitted) != 2 || env.emitted[1]["text"] != nil || env.emitted[1]["answer"] != nil {
		t.Fatalf("helper events = %v", env.emitted)
	}

	// A model that doesn't think says nothing.
	plain := &scriptedEnv{replies: []string{"hi"}}
	if text, _, _ := generate(context.Background(), plain, "primary", nil, true); text != "hi" {
		t.Fatalf("plain = %q", text)
	}
}
