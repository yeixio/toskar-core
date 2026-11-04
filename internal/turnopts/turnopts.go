// Package turnopts carries what an API request asks of one chat turn (spec
// §62): the caller's earlier messages, memory and knowledge choices, tool
// narrowing, and where progress goes. Chat in the app sets none of it, and
// behaves as before.
package turnopts

import (
	"context"
	"os"
	"path/filepath"
	"strings"

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

type liveKey struct{}

// WithLive asks for the answer as it's written, with replace chunks when
// what was shown changes (#1006).
func WithLive(ctx context.Context) context.Context {
	return context.WithValue(ctx, liveKey{}, true)
}

// LiveFrom says the turn's caller reads live text.
func LiveFrom(ctx context.Context) bool {
	live, _ := ctx.Value(liveKey{}).(bool)
	return live
}

type projectKey struct{}

// WithProject sets the folder a turn works in: where its commands run, and
// what its file tools reach (#1017).
func WithProject(ctx context.Context, dir string) context.Context {
	return context.WithValue(ctx, projectKey{}, dir)
}

// WithLongCommands has the turn's commands run as long as work needs, with
// their output sent on as it comes (#1017).
func WithLongCommands(ctx context.Context) context.Context {
	return context.WithValue(ctx, longCommandsKey{}, true)
}

// LongCommandsFrom says the turn's commands may run long.
func LongCommandsFrom(ctx context.Context) bool {
	long, _ := ctx.Value(longCommandsKey{}).(bool)
	return long
}

type longCommandsKey struct{}

// WithCommandOutput has a command pass on its output as it comes.
func WithCommandOutput(ctx context.Context, show func(string)) context.Context {
	return context.WithValue(ctx, commandOutputKey{}, show)
}

// CommandOutputFrom is where a command's output goes as it comes, or nil.
func CommandOutputFrom(ctx context.Context) func(string) {
	show, _ := ctx.Value(commandOutputKey{}).(func(string))
	return show
}

type commandOutputKey struct{}

// ProjectFrom is the folder a turn works in, or "".
func ProjectFrom(ctx context.Context) string {
	dir, _ := ctx.Value(projectKey{}).(string)
	return dir
}

// Project reads a folder a client on this computer named: its real, absolute
// path, or "" when it isn't one.
func Project(dir string) string {
	if !filepath.IsAbs(dir) {
		return ""
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return ""
	}
	if info, err := os.Stat(real); err != nil || !info.IsDir() {
		return ""
	}
	return real
}

// TrustsFilesIn reports whether what's read from a file in dir counts as the
// project's own. The home folder or the whole disk holds downloads and mail,
// other people's words, so being in one of those proves nothing.
func TrustsFilesIn(dir string) bool {
	if dir == "" || filepath.Dir(dir) == dir {
		return false
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	if real, err := filepath.EvalSymlinks(home); err == nil {
		home = real
	}
	// The home folder, or a folder that holds it.
	rel, err := filepath.Rel(dir, home)
	return err != nil || strings.HasPrefix(rel, "..")
}

// InProject reports a path inside dir, following links.
func InProject(dir, path string) bool {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
