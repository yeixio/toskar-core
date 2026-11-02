package tools

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/store"
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
	for i := range 2 {
		if !run("c", PolicyAskEachTime) {
			t.Errorf("call %d: after untrusted content, each call asks, and a yes for the session isn't kept", i+1)
		}
	}
	if !run("c", PolicyAsk) {
		t.Error("a yes under ask-each-time isn't kept, so c still asks")
	}
}

// A command that always asks does so whatever a profile or an earlier "always"
// says, and an automation, with no one to ask, doesn't run it (#1017).
func TestRegistryDestructiveAlwaysAsks(t *testing.T) {
	bus := events.NewBus(64)
	subID, ch := bus.Subscribe()
	defer bus.Unsubscribe(subID)
	r := &Registry{
		tools:   map[string]Tool{"terminal": stubTool{id: "terminal"}},
		policy:  NewPolicyEngine(),
		pending: map[string]*PendingCall{},
		bus:     bus,
	}
	meta := map[string]any{"conversation_id": "c1"}
	run := func(command, policy string, answer func(p map[string]any)) error {
		t.Helper()
		done := make(chan error, 1)
		go func() {
			_, err := r.Execute(context.Background(), "terminal", map[string]any{"command": command}, policy, "reason", meta)
			done <- err
		}()
		for {
			select {
			case err := <-done:
				return err
			case e := <-ch:
				if e.Type == events.ToolRequested {
					if answer == nil {
						t.Errorf("%q asked: %v", command, e.Payload)
						_ = r.Decide(e.Payload["request_id"].(string), false, false)
						continue
					}
					answer(e.Payload)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("%q didn't finish", command)
			}
		}
	}
	// "Always in this chat", on a known command, then a destructive one.
	always := func(p map[string]any) { _ = r.Decide(p["request_id"].(string), true, true) }
	if err := run("git status", PolicyAsk, always); err != nil {
		t.Fatal(err)
	}
	if err := run("go test ./...", PolicyAsk, nil); err != nil {
		t.Fatalf("always in this chat should cover a known command: %v", err)
	}
	asked := 0
	for _, policy := range []string{PolicyAsk, PolicyAllow} {
		// Answering "always" each time mustn't stick.
		for range 2 {
			err := run("sh -c 'rm -rf .'", policy, func(p map[string]any) {
				asked++
				if p["always_asks"] != true || p["cant_undo"] != true {
					t.Errorf("payload %v", p)
				}
				_ = r.Decide(p["request_id"].(string), true, true)
			})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if asked != 4 {
		t.Fatalf("asked %d times", asked)
	}
	err := run("unknown-tool", PolicyAllow, func(p map[string]any) {
		if p["always_asks"] != true || p["cant_undo"] != false {
			t.Errorf("payload %v", p)
		}
		_ = r.Decide(p["request_id"].(string), false, false)
	})
	if err == nil {
		t.Fatal("a no should stop it")
	}
	if _, err := r.Execute(context.Background(), "terminal", map[string]any{"command": "rm x"}, PolicyDeny, "r", meta); err == nil {
		t.Fatal("deny should still deny")
	}
	// An automation has no one to ask.
	_, err = r.Execute(context.Background(), "terminal", map[string]any{"command": "rm x"}, PolicyAllow, "r", map[string]any{"automation_id": "a1"})
	if !errors.Is(err, ErrNeedsApproval) {
		t.Fatalf("automation: %v", err)
	}
	if _, err := r.Execute(context.Background(), "terminal", map[string]any{"command": "git status"}, PolicyAllow, "r", map[string]any{"automation_id": "a1"}); err != nil {
		t.Fatalf("automation, known command: %v", err)
	}
}

// What ran, its result, and who said yes are kept with the chat (#1017).
func TestRegistryRecordsCommands(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	r := &Registry{
		tools:    map[string]Tool{"terminal": exitTool{code: 3}},
		policy:   NewPolicyEngine(),
		pending:  map[string]*PendingCall{},
		bus:      events.NewBus(8),
		auditLog: NewAuditLog(db.SQL),
	}
	long := "go test " + strings.Repeat("./x ", 100)
	if _, err := r.Execute(context.Background(), "terminal", map[string]any{"command": long}, PolicyAllow, "r", map[string]any{"conversation_id": "c1"}); err != nil {
		t.Fatal(err)
	}
	runs, err := r.auditLog.List(context.Background(), AuditFilter{ConversationID: "c1"})
	if err != nil || len(runs) != 1 {
		t.Fatalf("%v %v", runs, err)
	}
	run := runs[0]
	if run.Summary != strings.TrimSpace(long) || run.Status != RunFailed || run.Error != "exit 3" || run.Approval != ApprovedByProfile {
		t.Fatalf("%+v", run)
	}
}

type exitTool struct{ code int }

func (e exitTool) ID() string          { return "terminal" }
func (e exitTool) DisplayName() string { return "Terminal" }
func (e exitTool) Description() string { return "stub" }
func (e exitTool) Execute(context.Context, map[string]any) (map[string]any, error) {
	return map[string]any{"stdout": "", "exit": e.code}, nil
}

// After reading something from outside, the prompt says so (#1017).
func TestRegistrySaysWhyAfterOutside(t *testing.T) {
	bus := events.NewBus(8)
	subID, ch := bus.Subscribe()
	defer bus.Unsubscribe(subID)
	r := &Registry{tools: map[string]Tool{"terminal": stubTool{id: "terminal"}}, policy: NewPolicyEngine(), pending: map[string]*PendingCall{}, bus: bus}
	done := make(chan error, 1)
	go func() {
		_, err := r.Execute(context.Background(), "terminal", map[string]any{"command": "git status"}, PolicyAskEachTime, "r", map[string]any{"conversation_id": "c1"})
		done <- err
	}()
	for {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			return
		case e := <-ch:
			if e.Type == events.ToolRequested {
				if e.Payload["always_asks"] != true || e.Payload["after_outside"] != true {
					t.Errorf("payload %v", e.Payload)
				}
				_ = r.Decide(e.Payload["request_id"].(string), true, true)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("didn't finish")
		}
	}
}
