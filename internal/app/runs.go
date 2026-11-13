package app

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A turn keeps this much of a command's output, and this many steps, for an
// app that opens the chat later (#1017).
const (
	turnOutputLimit = 16 << 10
	turnStepLimit   = 200
)

// run is one conversation's turn in progress. Cancelling it stops every
// model call, tool call, plan step, and paired computer working on the turn,
// because they all share its context (spec §67).
type run struct {
	cancel  context.CancelFunc
	stopped atomic.Bool

	mu      sync.Mutex
	started time.Time
	person  string
	steps   []contracts.TurnStep
	output  string
	text    string
}

// note keeps what the turn just did (#1017).
func (r *run) note(evt events.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch evt.Type {
	case events.ToolStarted:
		id, _ := evt.Payload["tool_id"].(string)
		summary, _ := evt.Payload["summary"].(string)
		r.steps = append(r.steps, contracts.TurnStep{ToolID: id, Summary: summary, Status: "running"})
		if n := len(r.steps); n > turnStepLimit {
			r.steps = r.steps[n-turnStepLimit:]
		}
		r.output = ""
	case events.ToolOutput:
		text, _ := evt.Payload["text"].(string)
		r.output += text
		if n := len(r.output); n > turnOutputLimit {
			r.output = r.output[n-turnOutputLimit:]
		}
	case events.ToolCompleted, events.ToolFailed:
		id, _ := evt.Payload["tool_id"].(string)
		status := "completed"
		if evt.Type == events.ToolFailed {
			status = "failed"
		}
		for i := len(r.steps) - 1; i >= 0; i-- {
			if r.steps[i].ToolID == id && r.steps[i].Status == "running" {
				r.steps[i].Status = status
				break
			}
		}
	case events.ChatToken:
		content, _ := evt.Payload["content"].(string)
		if replace, _ := evt.Payload["replace"].(bool); replace {
			r.text = content
		} else {
			r.text += content
		}
	}
}

// startRun registers a conversation's turn so StopChat can reach it. The
// returned function unregisters it when the turn ends.
func (a *App) startRun(ctx context.Context, conversationID string) (context.Context, *run, func()) {
	ctx, cancel := context.WithCancel(ctx)
	r := &run{cancel: cancel, started: time.Now().UTC(), person: auth.PersonID(ctx)}
	if conversationID == "" {
		return ctx, r, cancel
	}
	ctx = events.WithNote(ctx, r.note)
	if prev, loaded := a.runs.Swap(conversationID, r); loaded {
		// A new turn in the same chat replaces one still running.
		prev.(*run).cancel()
	}
	return ctx, r, func() {
		a.runs.CompareAndDelete(conversationID, r)
		cancel()
	}
}

// StopChat stops a conversation's running turn, from any client: the page
// that started it, another window, or the API. It reports whether a turn was
// running.
func (a *App) StopChat(conversationID string) bool {
	v, ok := a.runs.Load(conversationID)
	if !ok {
		return false
	}
	r := v.(*run)
	r.stopped.Store(true)
	r.cancel()
	return true
}

// StopChatFor stops a conversation's running turn for the person asking:
// another person's chat, such as an agent of their swarm (#1135), reads as
// not running.
func (a *App) StopChatFor(ctx context.Context, conversationID string) bool {
	v, ok := a.runs.Load(conversationID)
	if !ok || v.(*run).person != auth.PersonID(ctx) {
		return false
	}
	return a.StopChat(conversationID)
}

// ChatTurn is what a conversation's turn has done so far (#1017). A chat
// that isn't this person's is an error, even while it's running.
func (a *App) ChatTurn(ctx context.Context, conversationID string) (contracts.ChatTurn, error) {
	if _, err := a.Conversations.Get(ctx, conversationID); err != nil {
		return contracts.ChatTurn{}, err
	}
	v, ok := a.runs.Load(conversationID)
	if !ok {
		return contracts.ChatTurn{}, nil
	}
	r := v.(*run)
	r.mu.Lock()
	defer r.mu.Unlock()
	started := r.started
	turn := contracts.ChatTurn{
		Running:   true,
		StartedAt: &started,
		Steps:     append([]contracts.TurnStep(nil), r.steps...),
		Output:    r.output,
		Text:      r.text,
	}
	if a.Tools != nil {
		turn.Waiting = a.Tools.Waiting(conversationID, r.person)
	}
	return turn, nil
}
