package simple

import (
	"context"
	"strings"

	"github.com/yeixio/toskar-core/internal/contextusage"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Events for an answer shown as it's written (#1006): agent.delta is more of
// it, and agent.replace takes the place of all of it, empty to take it back.
const (
	eventDelta   = "agent.delta"
	eventReplace = "agent.replace"
)

// EarlyAnswers is an environment that says whether a turn's answer may be
// seen before its checks: not one that must never show an off-topic answer,
// even briefly. An environment that doesn't say keeps its answers back.
type EarlyAnswers interface {
	MayShowEarly(ctx context.Context) bool
}

func mayShowEarly(ctx context.Context, env pluginapi.ExecutionEnvironment) bool {
	e, ok := env.(EarlyAnswers)
	return ok && e.MayShowEarly(ctx)
}

// liveAnswer shows one attempt at the answer as it's written. A nil one
// shows nothing, for a turn that keeps its answer until the end.
type liveAnswer struct {
	ch     chan<- pluginapi.OrchestrationEvent
	role   string
	nodeID string
	text   tools.LiveText
}

func (l *liveAnswer) send(eventType, content string) {
	l.ch <- pluginapi.OrchestrationEvent{Type: eventType, Role: l.role, NodeID: l.nodeID, Content: content}
}

func (l *liveAnswer) feed(piece string) {
	if l == nil {
		return
	}
	if delta := l.text.Feed(piece); delta != "" {
		l.send(eventDelta, delta)
	}
}

// takeBack starts a new attempt, taking back what the last one showed.
func (l *liveAnswer) takeBack() {
	if l == nil {
		return
	}
	if l.text.Shown() != "" {
		l.send(eventReplace, "")
	}
	l.text = tools.LiveText{}
}

// settle makes what's shown into the final text. It says whether it did: with
// nothing shown yet, the answer still has to be sent.
func (l *liveAnswer) settle(final string) bool {
	if l == nil || l.text.Shown() == "" {
		return false
	}
	shown := l.text.Shown()
	switch {
	case final == shown:
	case strings.HasPrefix(final, shown):
		l.send(eventDelta, final[len(shown):])
	default:
		l.send(eventReplace, final)
	}
	return true
}

// finish ends the turn with content as its answer, as streamText does.
func (l *liveAnswer) finish(ch chan<- pluginapi.OrchestrationEvent, role, nodeID, content string, metrics *pluginapi.GenerationMetrics, usage contextusage.Usage) {
	if l.settle(tools.VisibleText(content)) {
		completeText(ch, role, nodeID, tools.VisibleText(content), metrics, usage)
		return
	}
	streamText(ch, role, nodeID, content, metrics, usage)
}
