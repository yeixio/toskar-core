package tools

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/events"
)

// A swarm's agents ask as their own chats do (#1135): tool.requested says
// which swarm and agent asks, a swarm's questions are listed together, and
// one agent's "allow for this session" doesn't let another agent's call
// through.
func TestSwarmApprovals(t *testing.T) {
	bus := events.NewBus(8)
	r := &Registry{
		tools:   map[string]Tool{"terminal": stubTool{id: "terminal"}},
		policy:  NewPolicyEngine(),
		bus:     bus,
		pending: map[string]*PendingCall{},
	}
	subID, ch := bus.Subscribe()
	defer bus.Unsubscribe(subID)
	agent := func(n int) map[string]any {
		return map[string]any{"conversation_id": []string{"", "agent-1", "agent-2"}[n], "swarm_id": "s1",
			"agent": []string{"", "Agent 1: prices", "Agent 2: reviews"}[n], "agent_number": n}
	}
	// run executes as an agent and answers its question with allowSession;
	// it reports what the question carried, or nil when none came.
	run := func(n int, allowSession bool) map[string]any {
		t.Helper()
		errCh := make(chan error, 1)
		go func() {
			_, err := r.Execute(context.Background(), "terminal", map[string]any{"command": "ls"}, PolicyAsk, "test", agent(n))
			errCh <- err
		}()
		var asked map[string]any
		for {
			select {
			case evt := <-ch:
				if evt.Type == events.ToolRequested {
					asked = evt.Payload
					if got := r.WaitingInSwarm("s1", evt.Person); len(got) != 1 {
						t.Errorf("waiting in the swarm = %v", got)
					}
					if got := r.WaitingInSwarm("s2", evt.Person); len(got) != 0 {
						t.Errorf("waiting in another swarm = %v", got)
					}
					if err := r.Decide(evt.Payload["request_id"].(string), true, allowSession); err != nil {
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
	asked := run(1, true)
	if asked == nil || asked["swarm_id"] != "s1" || asked["agent"] != "Agent 1: prices" || asked["conversation_id"] != "agent-1" {
		t.Fatalf("agent 1's question = %v", asked)
	}
	if run(1, false) != nil {
		t.Error("allowed for the session, agent 1's next call asks again")
	}
	if asked := run(2, false); asked == nil || asked["agent"] != "Agent 2: reviews" {
		t.Errorf("agent 1's yes let agent 2's call through: %v", asked)
	}
}

// Confirm asks before Core does something itself, such as applying a swarm's
// changes to the person's folder: a profile that allows it doesn't ask, the
// person's no stops it, and with nobody to ask it needs approval.
func TestConfirm(t *testing.T) {
	bus := events.NewBus(8)
	r := &Registry{tools: map[string]Tool{}, policy: NewPolicyEngine(), bus: bus, pending: map[string]*PendingCall{}}
	subID, ch := bus.Subscribe()
	defer bus.Unsubscribe(subID)
	meta := map[string]any{"conversation_id": "coord", "swarm_id": "s1"}
	args := map[string]any{"path": "/src/app", "files": 2}
	if err := r.Confirm(context.Background(), "swarm.apply", args, PolicyAllow, "apply", meta); err != nil {
		t.Fatalf("allowed: %v", err)
	}
	confirm := func(allow bool) (map[string]any, error) {
		t.Helper()
		errCh := make(chan error, 1)
		go func() {
			errCh <- r.Confirm(context.Background(), "swarm.apply", args, PolicyAsk, "Apply the swarm's changes to /src/app: 2 files", meta)
		}()
		var asked map[string]any
		for {
			select {
			case evt := <-ch:
				if evt.Type == events.ToolRequested {
					asked = evt.Payload
					_ = r.Decide(evt.Payload["request_id"].(string), allow, false)
				}
			case err := <-errCh:
				return asked, err
			case <-time.After(2 * time.Second):
				t.Fatal("timed out")
			}
		}
	}
	asked, err := confirm(false)
	if err == nil || asked == nil || asked["reason"] != "Apply the swarm's changes to /src/app: 2 files" || asked["swarm_id"] != "s1" || asked["tool_id"] != "swarm.apply" {
		t.Fatalf("declined: %v, asked %v", err, asked)
	}
	if _, err := confirm(true); err != nil {
		t.Fatalf("allowed by the person: %v", err)
	}
	unattended := map[string]any{"automation_id": "a1"}
	if err := r.Confirm(context.Background(), "swarm.apply", args, PolicyAsk, "apply", unattended); !errors.Is(err, ErrNeedsApproval) {
		t.Fatalf("nobody to ask: %v", err)
	}
}
