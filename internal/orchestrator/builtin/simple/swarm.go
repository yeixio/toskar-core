package simple

import (
	"context"
	"fmt"
	"strings"

	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/swarm"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Swarms (#1135, docs/features/swarms.md): the coordinator's side. The
// planner splits the request into parts, or a race gives each model the
// whole request; the environment places the agents and runs each as a chat
// turn of its own (swarm.Runner); and this turn merges what they found. A
// split's answers are reference material for this turn's answer, as a plan's
// notes are, but whole. A race's answers are compared as Deliberate's drafts
// are: a short final answer most agree on wins, else they're cross-examined
// and judged.

// swarmGuidance tells the answering model how to use the agents' answers.
const swarmGuidance = "Several agents worked on parts of this request at the same time; each agent's whole answer to its part is in the reference material. " +
	"Use them to answer the whole request in one reply: combine them, keep what they found, and settle any differences between them. " +
	"Do not mention the agents or the parts."

// swarmAnswerRunes caps one agent's answer as reference material, so the
// answer sees every agent's.
const swarmAnswerRunes = 8000

// swarmAsked is the mode a turn's swarm runs in, and how many agents the
// message named, or "" for no swarm. A swarm is asked for on the turn (POST
// /chat's swarm, or "use a swarm" in the message), or by the profile's swarm
// strategy, which, like Team, leaves a quick question to be answered
// directly. Auto never chooses one, and an agent's own turn never starts
// another.
func swarmAsked(ctx context.Context, profile contracts.AIProfile, prompt string, escalate bool) (string, int) {
	if _, isAgent := swarm.AgentFrom(ctx); isAgent {
		return "", 0
	}
	asked := swarm.ModeFrom(ctx)
	named, phrased := huginn.AsksForSwarm(prompt)
	byProfile := profile.Orchestration.Strategy == profiles.StrategySwarm && escalate
	if asked == "" && !phrased && !byProfile {
		return "", 0
	}
	return swarm.ModeFor(asked, profile.Orchestration.Swarm, swarm.PolicyFrom(ctx)), named
}

// swarmBudget is a turn's swarm budget, with the agents a message named.
func swarmBudget(ctx context.Context, profile contracts.AIProfile, named int) swarm.Budget {
	b := swarm.BudgetFor(profile.Orchestration.Swarm, swarm.PolicyFrom(ctx), profile.Orchestration.MaxWorkers, false)
	if named > 0 {
		b.Agents = max(profiles.SwarmMinAgents, min(profiles.SwarmMaxAgents, named))
	}
	return b
}

// askSwarmPlanner asks the planner for a split's parts, each with a title. A
// planner that doesn't answer, or can't split the request, gives one part:
// the whole request.
func askSwarmPlanner(ctx context.Context, env pluginapi.ExecutionEnvironment, ch chan<- pluginapi.OrchestrationEvent, role, prompt string, maxParts int) []swarm.Part {
	whole := []swarm.Part{{Title: swarm.TitleOf(prompt), Instructions: prompt}}
	ask := []pluginapi.ChatMessage{
		{Role: "system", Content: fmt.Sprintf(swarm.PlannerInstructions, maxParts)},
		{Role: "user", Content: prompt},
	}
	announceRole(env, role)
	content, metrics, err := generateText(ctx, env, role, ask)
	if err != nil {
		return whole
	}
	reportRole(ch, env, role, metrics)
	parts := swarm.ParseParts(content, maxParts)
	env.Emit(EventPlanner, map[string]any{"parts": len(parts), "role": role})
	if len(parts) == 0 {
		return whole
	}
	return parts
}

// swarmTurn is what a swarm left for the rest of the turn.
type swarmTurn struct {
	// id is the swarm's record, to finish when the turn ends.
	id string
	// reference is a split's answers, for this turn's answer.
	reference string
	// answer is a race's answer, which is the turn's.
	answer string
	// winner is a race's winning agent, or 0.
	winner int
	// ran says the agents ran and something came of them; otherwise the
	// turn answers as an ordinary one.
	ran bool
	// single says the swarm ran as one agent, by design: this turn.
	single bool
}

// runSwarm plans a swarm, has the environment run its agents, and merges a
// race's answers. A split's answers come back as reference material.
func runSwarm(ctx context.Context, env pluginapi.ExecutionEnvironment, runner swarm.Runner, ch chan<- pluginapi.OrchestrationEvent,
	profile contracts.AIProfile, role, mode, prompt, instructions string, named int) swarmTurn {
	b := swarmBudget(ctx, profile, named)
	req := swarm.Request{Mode: mode, Prompt: prompt, Budget: b}
	if mode == swarm.ModeRace {
		req.Models = swarm.RaceModels(profile.Orchestration.Swarm, swarm.PolicyFrom(ctx), b.Agents)
		req.Parts = swarm.RaceParts(prompt, len(req.Models))
	} else {
		req.Parts = askSwarmPlanner(ctx, env, ch, plannerRole(profile, role), prompt, b.Agents)
	}
	if ctx.Err() != nil {
		return swarmTurn{}
	}
	out, err := runner.RunSwarm(ctx, req)
	if err != nil || out.Single != "" {
		// The environment said why, and the turn answers as one agent.
		return swarmTurn{id: out.ID, single: err == nil}
	}
	turn := swarmTurn{id: out.ID}
	done := out.Done()
	if len(done) == 0 || ctx.Err() != nil {
		return turn
	}
	agents := make([]int, len(done))
	for k, i := range done {
		agents[k] = out.Agents[i].N
	}
	env.Emit(swarm.EventMerging, map[string]any{"swarm_id": out.ID, "mode": mode, "agents": agents})
	turn.ran = true
	if mode != swarm.ModeRace {
		var b strings.Builder
		b.WriteString("Answers from each agent:")
		for _, i := range done {
			a, r := out.Agents[i], out.Results[i]
			fmt.Fprintf(&b, "\n\nAgent %d, %s (%s):\n%s", a.N, a.Part.Title, a.Part.Instructions, clip(strings.TrimSpace(r.Answer), swarmAnswerRunes))
		}
		turn.reference = b.String()
		return turn
	}
	turn.answer, turn.winner = raceAnswer(ctx, env, profile, role, prompt, instructions, out, done)
	return turn
}

// raceAnswer picks or merges a race's answers, as Deliberate does its drafts:
// a short final answer most agents agree on wins; otherwise each answer is
// checked by another agent and a judge writes the answer. It returns the
// winning agent, or 0 when the judge merged them.
func raceAnswer(ctx context.Context, env pluginapi.ExecutionEnvironment, profile contracts.AIProfile, role, prompt, instructions string, out swarm.Outcome, done []int) (string, int) {
	drafts := make([]draft, len(done))
	for k, i := range done {
		text := strings.TrimSpace(out.Results[i].Answer)
		drafts[k] = draft{role: out.Agents[i].Role, node: out.Agents[i].NodeID, text: text, final: draftFinal(text)}
	}
	if len(drafts) == 1 {
		return drafts[0].text, out.Agents[done[0]].N
	}
	outcome, _, kept := vote(drafts)
	if outcome == OutcomeAgreed || outcome == OutcomeMajority {
		return drafts[kept].text, out.Agents[done[kept]].N
	}
	critiques := crossExamine(ctx, env, prompt, drafts)
	if ctx.Err() == nil && len(critiques) > 0 {
		judgeRole := judgeRoleFor(profile, role)
		announceRole(env, judgeRole)
		if text, err := judge(ctx, env, judgeRole, prompt, instructions, drafts, critiques); err == nil && ctx.Err() == nil {
			return text, 0
		}
	}
	// Nothing to judge with: the first agent's answer stands.
	return drafts[0].text, out.Agents[done[0]].N
}
