package simple

import (
	"context"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/runlog"
	"github.com/yeixio/toskar-core/internal/swarm"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// swarmEnv is a roleEnv that runs swarms: it records the planned request and
// answers with the agents' results it was given, as the chat environment
// would after running their turns.
type swarmEnv struct {
	*roleEnv
	asked    []swarm.Request
	single   string
	answers  []string
	statuses []string
	finished []string
	winner   int
}

func (e *swarmEnv) RunSwarm(_ context.Context, req swarm.Request) (swarm.Outcome, error) {
	e.asked = append(e.asked, req)
	out := swarm.Outcome{ID: "s1", Single: e.single}
	if e.single != "" {
		return out, nil
	}
	for i, part := range req.Parts {
		out.Agents = append(out.Agents, swarm.Agent{N: i + 1, Role: swarm.AgentRole(i + 1), Part: part, NodeID: "pc-1"})
		r := swarm.Result{Status: swarm.AgentDone}
		if i < len(e.answers) {
			r.Answer = e.answers[i]
		}
		if i < len(e.statuses) {
			r.Status = e.statuses[i]
		}
		out.Results = append(out.Results, r)
	}
	return out, nil
}

func (e *swarmEnv) FinishSwarm(_ context.Context, id, status string, winner int) {
	e.finished = append(e.finished, id+":"+status)
	e.winner = winner
}

func runSwarmTurn(t *testing.T, ctx context.Context, env *swarmEnv, prompt string, orch contracts.OrchestrationPolicy) string {
	t.Helper()
	orch.Verification = "off"
	events, err := New().Run(ctx, contracts.Task{Prompt: prompt}, contracts.AIProfile{
		Roles: []contracts.ModelRole{{Role: "assistant", ModelID: "m"}}, Orchestration: orch,
	}, env)
	if err != nil {
		t.Fatal(err)
	}
	var text string
	for evt := range events {
		if evt.Type == "agent.message" {
			text += evt.Content
		}
	}
	return text
}

const kayaks = "Which sea kayak should I buy for Juneau: compare prices, reviews, and how each handles cold water."

// A split: the planner's parts go to the agents, and the answer is written
// from their whole answers, as reference material.
func TestSwarmSplit(t *testing.T) {
	inner := newRoleEnv(map[string]string{
		"assistant": `{"parts": [{"title": "Prices", "instructions": "Compare the prices."}, {"title": "Reviews", "instructions": "Sum up the reviews."}]}`,
	})
	inner.sequence["assistant"] = []string{"The Tempest is the best fit."}
	env := &swarmEnv{roleEnv: inner, answers: []string{"The Tempest costs $2,400.", "Reviewers love the Tempest in cold water."}}
	answer := runSwarmTurn(t, swarm.WithMode(context.Background(), swarm.ModeSplit), env, kayaks, contracts.OrchestrationPolicy{})
	if answer != "The Tempest is the best fit." {
		t.Fatalf("answer = %q", answer)
	}
	if len(env.asked) != 1 || len(env.asked[0].Parts) != 2 || env.asked[0].Parts[1].Title != "Reviews" || env.asked[0].Mode != swarm.ModeSplit {
		t.Fatalf("asked = %+v", env.asked)
	}
	if env.asked[0].Budget.Agents != 4 || env.asked[0].Budget.Tokens != 400_000 {
		t.Errorf("budget = %+v", env.asked[0].Budget)
	}
	final := inner.prompts["assistant"][len(inner.prompts["assistant"])-1]
	for _, want := range []string{"Answers from each agent", "Agent 1, Prices", "$2,400", "Reviewers love the Tempest"} {
		if !strings.Contains(final, want) {
			t.Errorf("the answer's prompt lacks %q:\n%s", want, final)
		}
	}
	if !strings.Contains(inner.systems["assistant"], "Split the request") {
		t.Errorf("the planner wasn't asked for parts: %q", inner.systems["assistant"])
	}
	if env.count(swarm.EventMerging) != 1 || len(env.finished) != 1 || env.finished[0] != "s1:done" {
		t.Errorf("merging %d, finished %v", env.count(swarm.EventMerging), env.finished)
	}
	// "Use 3 agents" in the message asks for a swarm of three.
	inner2 := newRoleEnv(map[string]string{"assistant": `{"parts": ["a", "b", "c", "d"]}`})
	env2 := &swarmEnv{roleEnv: inner2, answers: []string{"A", "B", "C"}}
	runSwarmTurn(t, context.Background(), env2, kayaks+" Use 3 agents.", contracts.OrchestrationPolicy{})
	if len(env2.asked) != 1 || env2.asked[0].Budget.Agents != 3 || len(env2.asked[0].Parts) != 3 {
		t.Fatalf("use 3 agents: %+v", env2.asked)
	}
}

// A race whose answers disagree is cross-examined by the agents' models and
// judged; one whose answers agree keeps the agreeing answer, and its agent
// wins.
func TestSwarmRace(t *testing.T) {
	race := contracts.OrchestrationPolicy{Strategy: "swarm", Swarm: &contracts.SwarmPolicy{Mode: swarm.ModeRace, Models: []string{"qwen", "gemma"}}}
	inner := newRoleEnv(map[string]string{
		"agent:1": `{"claims": [], "disagreements": ["B says the ferry is faster"], "likely_errors": []}`,
		"agent:2": `{"claims": [], "disagreements": ["A says the flight is faster"], "likely_errors": ["A ignores the weather"]}`,
	})
	inner.replies["assistant"] = "Take the ferry when the weather is bad, and fly otherwise."
	env := &swarmEnv{roleEnv: inner, answers: []string{"Fly: it takes 45 minutes and costs about $250.", "Take the ferry: it runs daily and handles weather."}}
	answer := runSwarmTurn(t, swarm.WithMode(context.Background(), swarm.ModeRace), env, "Ferry or flight from Juneau to Haines?", race)
	if answer != "Take the ferry when the weather is bad, and fly otherwise." {
		t.Fatalf("answer = %q", answer)
	}
	if len(env.asked) != 1 || len(env.asked[0].Parts) != 2 || env.asked[0].Models[1] != "gemma" || env.asked[0].Parts[0].Instructions != "Ferry or flight from Juneau to Haines?" {
		t.Fatalf("asked = %+v", env.asked)
	}
	if inner.calls["agent:1"] != 1 || inner.calls["agent:2"] != 1 {
		t.Errorf("critiques: %v", inner.calls)
	}
	if !strings.Contains(inner.systems["assistant"], "You are the judge") || env.winner != 0 {
		t.Errorf("not judged: winner %d, system %q", env.winner, inner.systems["assistant"])
	}

	agree := newRoleEnv(nil)
	env = &swarmEnv{roleEnv: agree, answers: []string{"It's 391.\nFinal answer: 391", "17 × 23 = 391\nFinal answer: 391"}}
	answer = runSwarmTurn(t, swarm.WithMode(context.Background(), swarm.ModeRace), env, "What's 17 × 23? Show the steps.", race)
	if !strings.Contains(answer, "It's 391.") || env.winner != 1 || agree.calls["assistant"] != 0 {
		t.Fatalf("agreed: %q, winner %d, calls %v", answer, env.winner, agree.calls)
	}

	// An agent that ran out of budget doesn't win: the one that finished does.
	partial := newRoleEnv(nil)
	env = &swarmEnv{roleEnv: partial, answers: []string{"half an answ", "The whole answer."}, statuses: []string{swarm.AgentOutOfBudget, swarm.AgentDone}}
	if answer := runSwarmTurn(t, swarm.WithMode(context.Background(), swarm.ModeRace), env, "Ferry or flight?", race); answer != "The whole answer." || env.winner != 2 {
		t.Fatalf("out of budget: %q, winner %d", answer, env.winner)
	}
}

// One computer and one model: the swarm runs as one agent, the turn answers
// as an ordinary one, and nothing is merged.
func TestSwarmSingle(t *testing.T) {
	inner := newRoleEnv(map[string]string{"assistant": `{"parts": ["a", "b"]}`})
	inner.sequence["assistant"] = []string{"One agent's answer."}
	env := &swarmEnv{roleEnv: inner, single: swarm.OneAtATime}
	answer := runSwarmTurn(t, swarm.WithMode(context.Background(), swarm.ModeSplit), env, kayaks, contracts.OrchestrationPolicy{})
	if answer != "One agent's answer." {
		t.Fatalf("answer = %q", answer)
	}
	if env.count(swarm.EventMerging) != 0 || len(env.finished) != 1 || env.finished[0] != "s1:done" {
		t.Errorf("merging %d, finished %v", env.count(swarm.EventMerging), env.finished)
	}
}

// A task runs as one task and says a swarm for tasks comes later; an agent's
// own turn never starts a swarm; and Auto never picks one.
func TestSwarmNotHere(t *testing.T) {
	task := &swarmEnv{roleEnv: newRoleEnv(map[string]string{"assistant": "Fixed it."})}
	ctx := runlog.WithOrigin(swarm.WithMode(context.Background(), swarm.ModeSplit), runlog.Origin{Client: "terminal"})
	runSwarmTurn(t, ctx, task, "fix the failing test in parser_test.go", contracts.OrchestrationPolicy{})
	if len(task.asked) != 0 {
		t.Fatalf("a task ran a swarm: %+v", task.asked)
	}
	if note := task.last(swarm.EventNote); note == nil || note["reason"] != swarm.TaskLater {
		t.Fatalf("no note for the task: %v", note)
	}

	agent := &swarmEnv{roleEnv: newRoleEnv(map[string]string{"assistant": "My part."})}
	ctx = swarm.WithAgent(swarm.WithMode(context.Background(), swarm.ModeSplit), swarm.AgentRef{SwarmID: "s1", N: 2})
	if answer := runSwarmTurn(t, ctx, agent, kayaks+" Use a swarm.", contracts.OrchestrationPolicy{Strategy: "swarm"}); answer != "My part." || len(agent.asked) != 0 {
		t.Fatalf("an agent started a swarm: %q %+v", answer, agent.asked)
	}

	auto := &swarmEnv{roleEnv: newRoleEnv(map[string]string{"assistant": "An answer."})}
	runSwarmTurn(t, context.Background(), auto, kayaks, contracts.OrchestrationPolicy{})
	if len(auto.asked) != 0 {
		t.Fatalf("Auto picked a swarm: %+v", auto.asked)
	}
}
