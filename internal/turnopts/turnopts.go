// Package turnopts carries what an API request asks of one chat turn (spec
// §62): the caller's earlier messages, memory and knowledge choices, tool
// narrowing, and where progress goes. Chat in the app sets none of it, and
// behaves as before.
package turnopts

import (
	"context"

	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Options are one turn's choices from an API request, already limited to
// what the caller's key allows.
type Options struct {
	// History is the conversation before the last user message, as the
	// caller sent it.
	History []pluginapi.ChatMessage
	// System is the caller's system messages, joined.
	System string
	// Memory uses the person's memories for this turn.
	Memory bool
	// Knowledge uses the profile's connected knowledge; KnowledgeSources
	// adds more.
	Knowledge        bool
	KnowledgeSources []string
	// Tools, when not nil, narrows the profile's tools to these ids; an
	// empty list allows none.
	Tools []string
	// ReadOnlyTools drops tools that change anything.
	ReadOnlyTools bool
	// Progress, when set, receives the turn's progress and tool activity.
	Progress func(eventType string, payload map[string]any)
	// Meta, when set, receives the answer's sources, steps, and notice.
	Meta func(*contracts.MessageMeta)
	// FixedProfile keeps the turn's profile and model as given, whatever
	// the conversation was set to, as a chat portal's are (#205).
	FixedProfile bool
	// Language answers in this language, such as a portal's; empty
	// follows the person and the message.
	Language string
	// Portal is the chat portal the turn is a visitor's in (#205), which
	// waits for the people who run Toskar.
	Portal string
	// ExactModel answers with the model the request named, or fails: no
	// other model stands in for it when it can't answer (#871). The same
	// model on another computer still may.
	ExactModel bool
}

type key struct{}

// With carries options for a turn.
func With(ctx context.Context, o *Options) context.Context { return context.WithValue(ctx, key{}, o) }

// From returns a turn's options, or nil for an ordinary chat.
func From(ctx context.Context) *Options {
	o, _ := ctx.Value(key{}).(*Options)
	return o
}

// Branch is where in a chat a turn answers from (#447): after ParentID, as
// a retry of the answer RetryOf, or with the edited message EditOf, each a
// new version of that point. The zero Branch goes on at the end.
type Branch struct {
	ParentID string
	RetryOf  string
	EditOf   string
}

// Any reports a turn that answers from a point other than the end.
func (b Branch) Any() bool { return b.ParentID != "" || b.RetryOf != "" || b.EditOf != "" }

type branchKey struct{}

// WithBranch carries where a turn answers from.
func WithBranch(ctx context.Context, b Branch) context.Context {
	return context.WithValue(ctx, branchKey{}, b)
}

// BranchFrom is where a turn answers from; the zero Branch is the end.
func BranchFrom(ctx context.Context) Branch {
	b, _ := ctx.Value(branchKey{}).(Branch)
	return b
}
