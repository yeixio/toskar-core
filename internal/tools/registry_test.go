package tools

import (
	"context"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/events"
)

type stubTool struct {
	id string
}

func (t stubTool) ID() string          { return t.id }
func (t stubTool) DisplayName() string { return t.id }
func (t stubTool) Description() string { return "stub" }
func (t stubTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	return map[string]any{"ok": true, "args": args}, nil
}

func TestRegistryAskDecideAllow(t *testing.T) {
	bus := events.NewBus(8)
	r := &Registry{
		tools:   map[string]Tool{"terminal": stubTool{id: "terminal"}},
		policy:  NewPolicyEngine(),
		bus:     bus,
		pending: map[string]*PendingCall{},
	}

	subID, ch := bus.Subscribe()
	defer bus.Unsubscribe(subID)

	errCh := make(chan error, 1)
	go func() {
		_, err := r.Execute(context.Background(), "terminal", map[string]any{"command": "echo hi"}, PolicyAsk, "test", map[string]any{
			"conversation_id": "conv-1",
		})
		errCh <- err
	}()

	var requestID string
	deadline := time.After(2 * time.Second)
	for requestID == "" {
		select {
		case evt := <-ch:
			if evt.Type == events.ToolRequested {
				id, _ := evt.Payload["request_id"].(string)
				if id == "" {
					t.Fatal("missing request_id")
				}
				if evt.Payload["conversation_id"] != "conv-1" {
					t.Fatalf("conversation_id=%v", evt.Payload["conversation_id"])
				}
				requestID = id
			}
		case <-deadline:
			t.Fatal("timed out waiting for tool.requested")
		}
	}

	if err := r.Decide(requestID, true, false); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Execute")
	}
}

func TestRegistryAskDecideDeny(t *testing.T) {
	bus := events.NewBus(8)
	r := &Registry{
		tools:   map[string]Tool{"terminal": stubTool{id: "terminal"}},
		policy:  NewPolicyEngine(),
		bus:     bus,
		pending: map[string]*PendingCall{},
	}

	subID, ch := bus.Subscribe()
	defer bus.Unsubscribe(subID)

	errCh := make(chan error, 1)
	go func() {
		_, err := r.Execute(context.Background(), "terminal", map[string]any{"command": "echo hi"}, PolicyAsk, "test", nil)
		errCh <- err
	}()

	var requestID string
	deadline := time.After(2 * time.Second)
	for requestID == "" {
		select {
		case evt := <-ch:
			if evt.Type == events.ToolRequested {
				requestID, _ = evt.Payload["request_id"].(string)
			}
		case <-deadline:
			t.Fatal("timed out waiting for tool.requested")
		}
	}
	if err := r.Decide(requestID, false, false); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	select {
	case err := <-errCh:
		if err == nil || err.Error() != `tool "terminal" denied by user` {
			t.Fatalf("Execute err=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Execute")
	}
}

// Allowed for the session, a tool runs in that chat without asking again,
// and asks in another chat and when the chat read untrusted content (#959).
func TestRegistryAllowForSessionIsPerChat(t *testing.T) {
	bus := events.NewBus(8)
	r := &Registry{
		tools:   map[string]Tool{"terminal": stubTool{id: "terminal"}},
		policy:  NewPolicyEngine(),
		bus:     bus,
		pending: map[string]*PendingCall{},
	}
	subID, ch := bus.Subscribe()
	defer bus.Unsubscribe(subID)

	// run executes in conv and answers its prompt, if one comes, with
	// allow for session; it reports whether one came.
	run := func(conv, policy string) bool {
		t.Helper()
		errCh := make(chan error, 1)
		go func() {
			_, err := r.Execute(context.Background(), "terminal", map[string]any{"command": "ls"}, policy, "test", map[string]any{"conversation_id": conv})
			errCh <- err
		}()
		asked := false
		for {
			select {
			case evt := <-ch:
				if evt.Type == events.ToolRequested {
					asked = true
					id, _ := evt.Payload["request_id"].(string)
					if err := r.Decide(id, true, true); err != nil {
						t.Fatal(err)
					}
				}
			case err := <-errCh:
				if err != nil {
					t.Fatal(err)
				}
				return asked
			case <-time.After(2 * time.Second):
				t.Fatal("timed out")
			}
		}
	}
	if !run("a", PolicyAsk) {
		t.Fatal("the first call asks")
	}
	if run("a", PolicyAsk) {
		t.Error("allowed for the session, the next call in that chat runs without asking")
	}
	if !run("b", PolicyAsk) {
		t.Error("another chat asks")
	}
	if !run("c", PolicyAskEachTime) || !run("c", PolicyAskEachTime) {
		t.Error("after untrusted content, each call asks, and a yes for the session isn't kept")
	}
	if !run("c", PolicyAsk) {
		t.Error("a yes under ask-each-time isn't kept, so c still asks")
	}
}
