package app

import (
	"context"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/events"
)

// A turn keeps what an app opening it later needs: its steps, the end of the
// running command's output, and the answer so far (#1017).
func TestRunNotesTheTurn(t *testing.T) {
	r := &run{}
	note := func(kind string, payload map[string]any) { r.note(events.New(kind, payload)) }

	note(events.ToolStarted, map[string]any{"tool_id": "terminal", "summary": "go test"})
	note(events.ToolOutput, map[string]any{"text": "ok\n"})
	if r.output != "ok\n" || len(r.steps) != 1 || r.steps[0].Status != "running" {
		t.Fatalf("running: output %q, steps %+v", r.output, r.steps)
	}
	note(events.ToolOutput, map[string]any{"text": strings.Repeat("x", turnOutputLimit+10)})
	if len(r.output) != turnOutputLimit || strings.HasPrefix(r.output, "ok") {
		t.Errorf("kept %d bytes of output, want the last %d", len(r.output), turnOutputLimit)
	}
	note(events.ToolCompleted, map[string]any{"tool_id": "terminal"})
	if r.steps[0].Status != "completed" {
		t.Errorf("status = %q", r.steps[0].Status)
	}
	// A call refused before it started has no step to end.
	note(events.ToolFailed, map[string]any{"tool_id": "filesystem.read", "malformed": true})
	if len(r.steps) != 1 {
		t.Errorf("steps = %+v", r.steps)
	}

	// The next command's output starts over.
	note(events.ToolStarted, map[string]any{"tool_id": "terminal", "summary": "ls"})
	if r.output != "" {
		t.Errorf("output carried over: %q", r.output)
	}
	note(events.ToolFailed, map[string]any{"tool_id": "terminal"})
	if r.steps[1].Status != "failed" {
		t.Errorf("status = %q", r.steps[1].Status)
	}

	note(events.ChatToken, map[string]any{"content": "Fix"})
	note(events.ChatToken, map[string]any{"content": "ing"})
	if r.text != "Fixing" {
		t.Errorf("text = %q", r.text)
	}
	note(events.ChatToken, map[string]any{"content": "Fixed", "replace": true})
	if r.text != "Fixed" {
		t.Errorf("after replace, text = %q", r.text)
	}

	for i := 0; i < turnStepLimit+5; i++ {
		note(events.ToolStarted, map[string]any{"tool_id": "terminal"})
	}
	if len(r.steps) != turnStepLimit {
		t.Errorf("kept %d steps, want %d", len(r.steps), turnStepLimit)
	}
}

// What a turn publishes reaches its run, with the bus too busy or absent.
func TestStartRunHearsItsEvents(t *testing.T) {
	a := &App{}
	ctx, r, end := a.startRun(context.Background(), "c1")
	defer end()
	events.NewBus(1).PublishFor(ctx, events.New(events.ToolStarted, map[string]any{"tool_id": "terminal"}))
	if len(r.steps) != 1 {
		t.Fatalf("steps = %+v", r.steps)
	}
}
