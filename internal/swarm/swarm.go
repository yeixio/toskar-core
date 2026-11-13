// Package swarm has several agents work on one request at once (#1135,
// docs/features/swarms.md). A coordinator plans the parts, places each agent
// on a computer and model, runs them, and merges what they found.
//
// Each agent is an ordinary chat turn in a chat of its own, in the
// coordinator chat's project, so its tools, approvals, traces, and messages
// work as any chat's do, and the person can open and continue it. This
// package holds what doesn't depend on how a turn runs: the plan's parts,
// placement into lanes, the budgets, running the lanes, and the store. The
// chat environment runs the agents' turns (Runner), and the orchestrator
// plans and merges.
//
// This version runs swarms on questions: turns with no folder. A task's
// agents, Git worktrees, and merging code come next.
package swarm

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Modes a swarm runs in.
const (
	// ModeSplit splits the job into parts that agents do at once.
	ModeSplit = profiles.SwarmSplit
	// ModeRace has agents on different models each do the whole request.
	ModeRace = profiles.SwarmRace
)

// A swarm's status.
const (
	StatusRunning = "running"
	StatusMerging = "merging"
	StatusDone    = "done"
	StatusStopped = "stopped"
	StatusFailed  = "failed"
)

// An agent's status.
const (
	AgentQueued      = "queued"
	AgentRunning     = "running"
	AgentDone        = "done"
	AgentFailed      = "failed"
	AgentStopped     = "stopped"
	AgentOutOfBudget = "out_of_budget"
)

// Events on the event stream, each with swarm_id (docs/features/swarms.md#events).
const (
	// EventStarted: mode, and agents: agent, conversation_id, part, role,
	// model, node_id, status (running or queued).
	EventStarted = "swarm.started"
	// EventAgentStarted: agent, node_id, model.
	EventAgentStarted = "swarm.agent.started"
	// EventAgentProgress: agent, step, tool, summary; one per tool call.
	EventAgentProgress = "swarm.agent.progress"
	// EventAgentFinished: agent, status, summary, tokens, seconds.
	EventAgentFinished = "swarm.agent.finished"
	// EventMerging: what is being merged.
	EventMerging = "swarm.merging"
	// EventFinished: status, winner (race), totals.
	EventFinished = "swarm.finished"
	// EventNote is a swarm asked for that ran differently, with the reason:
	// as one agent, or a task run as one today.
	EventNote = "swarm.note"
)

// ValidMode reports a mode a person can ask for.
func ValidMode(mode string) bool { return mode == ModeSplit || mode == ModeRace }

// AgentRole is agent N's role: "agent:N", from 1.
func AgentRole(n int) string { return fmt.Sprintf("%s:%d", profiles.RoleAgent, n) }

// AgentNumber is an agent role's number, or 0 for another role.
func AgentNumber(role string) int {
	rest, ok := strings.CutPrefix(role, profiles.RoleAgent+":")
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 1 {
		return 0
	}
	return n
}

// Label names an agent for people: "Agent 2: tests".
func Label(n int, title string) string {
	if title = strings.TrimSpace(title); title != "" {
		return fmt.Sprintf("Agent %d: %s", n, title)
	}
	return fmt.Sprintf("Agent %d", n)
}

type modeKey struct{}

// WithMode asks for a swarm on one turn: split or race (POST /chat's swarm).
func WithMode(ctx context.Context, mode string) context.Context {
	if mode == "" {
		return ctx
	}
	return context.WithValue(ctx, modeKey{}, mode)
}

// ModeFrom is the swarm a turn asked for, or "".
func ModeFrom(ctx context.Context) string {
	mode, _ := ctx.Value(modeKey{}).(string)
	return mode
}

type policyKey struct{}

// WithPolicy overrides the profile's swarm controls for one turn, such as a
// budget POST /swarms was sent.
func WithPolicy(ctx context.Context, p *contracts.SwarmPolicy) context.Context {
	if p == nil {
		return ctx
	}
	return context.WithValue(ctx, policyKey{}, p)
}

// PolicyFrom is a turn's own swarm controls, or nil.
func PolicyFrom(ctx context.Context) *contracts.SwarmPolicy {
	p, _ := ctx.Value(policyKey{}).(*contracts.SwarmPolicy)
	return p
}

type idKey struct{}

// WithID has a turn's swarm use a record already made, by POST /swarms, so
// the caller has its id before the agents start.
func WithID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, idKey{}, id)
}

// IDFrom is the swarm record a turn was given, or "".
func IDFrom(ctx context.Context) string {
	id, _ := ctx.Value(idKey{}).(string)
	return id
}

// AgentRef is what an agent's own turn knows about its place in a swarm.
type AgentRef struct {
	SwarmID string
	// N is its number, from 1.
	N int
	// Label names it for people, such as "Agent 2: tests".
	Label string
	// NodeID and ModelID are where placement put it; its turn runs there.
	NodeID  string
	ModelID string
}

type agentKey struct{}

// WithAgent marks a turn as a swarm agent's.
func WithAgent(ctx context.Context, a AgentRef) context.Context {
	return context.WithValue(ctx, agentKey{}, a)
}

// AgentFrom is the swarm agent a turn is, if it is one. An agent's turn never
// starts a swarm of its own.
func AgentFrom(ctx context.Context) (AgentRef, bool) {
	a, ok := ctx.Value(agentKey{}).(AgentRef)
	return a, ok
}
