package app

import (
	"context"
	"sync/atomic"
)

// run is one conversation's turn in progress. Cancelling it stops every
// model call, tool call, plan step, and paired computer working on the turn,
// because they all share its context (spec §67).
type run struct {
	cancel  context.CancelFunc
	stopped atomic.Bool
}

// startRun registers a conversation's turn so StopChat can reach it. The
// returned function unregisters it when the turn ends.
func (a *App) startRun(ctx context.Context, conversationID string) (context.Context, *run, func()) {
	ctx, cancel := context.WithCancel(ctx)
	r := &run{cancel: cancel}
	if conversationID == "" {
		return ctx, r, cancel
	}
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
