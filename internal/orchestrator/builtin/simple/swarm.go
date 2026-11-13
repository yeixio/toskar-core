package simple

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/structured"
	"github.com/yeixio/toskar-core/internal/swarm"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/internal/untrusted"
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

// swarmTaskGuidance tells the answering model how to report a task swarm.
const swarmTaskGuidance = "Several agents worked on this task at the same time, and Toskar merged their changes; each agent's report is in the reference material. " +
	"Tell the person what was done, from the reports: what changed and how it was checked, briefly. Where a report and the files Git saw disagree, the files are right: say plainly what wasn't done. Don't claim more than that, and don't mention the agents' parts one by one unless it helps."

// swarmBudget is a turn's swarm budget, with the agents a message named. A
// task gets a task's time.
func swarmBudget(ctx context.Context, profile contracts.AIProfile, named int, task bool) swarm.Budget {
	b := swarm.BudgetFor(profile.Orchestration.Swarm, swarm.PolicyFrom(ctx), profile.Orchestration.MaxWorkers, task)
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

// planLooks caps the coordinator's reads before it plans a task, and
// planListing the files it's shown.
const (
	planLooks     = 4
	planListing   = 200
	planReadRunes = 6000
)

// lookTools are what the coordinator may use to look at a task's folder
// before it plans: reading only.
var lookTools = []string{"filesystem.read", "filesystem.search"}

// askTaskPlanner asks the planner for a task's parts after a short, read-only
// look at the folder: its files are listed, and it can read some. Parts that
// expect to change the same paths go to one agent. A planner that doesn't
// answer, or can't split the task, gives one part: the whole task.
func askTaskPlanner(ctx context.Context, env pluginapi.ExecutionEnvironment, ch chan<- pluginapi.OrchestrationEvent, profile contracts.AIProfile, role, prompt, folder string, maxParts int) []swarm.Part {
	whole := []swarm.Part{{Title: swarm.TitleOf(prompt), Instructions: prompt}}
	look := offerOnly(profile, lookTools)
	system := fmt.Sprintf(swarm.TaskPlannerInstructions, maxParts)
	if toolPrompt := tools.PromptFor(look); toolPrompt != "" && !profile.ModelCallsNoTools {
		system += "\n" + toolPrompt
	}
	files := swarm.ListFiles(ctx, folder, planListing)
	listing := "(none)"
	if len(files) > 0 {
		listing = strings.Join(files, "\n")
	}
	ask := []pluginapi.ChatMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: "The task:\n" + prompt + "\n\nFiles in the folder:\n" + listing},
	}
	announceRole(env, role)
	reads := 0
	for {
		content, metrics, err := generateText(ctx, env, role, ask)
		if err != nil || ctx.Err() != nil {
			return whole
		}
		reportRole(ch, env, role, metrics)
		parsed := tools.ParseModelOutput(content)
		if parsed.Call != nil {
			id := tools.Canonical(parsed.Call.ID)
			if reads >= planLooks || !toolEnabled(look, id) {
				if reads > planLooks {
					return whole
				}
				// Enough reading, or a tool it can't use here: the plan now.
				reads = planLooks + 1
				ask = append(ask, pluginapi.ChatMessage{Role: "assistant", Content: content},
					pluginapi.ChatMessage{Role: "user", Content: "Reply with the parts as JSON now, without reading more."})
				continue
			}
			reads++
			note := "Tool result:\n"
			if result, err := env.ExecuteTool(ctx, id, parsed.Call.Args); err != nil {
				note = "The tool failed: " + publicToolError(err)
			} else {
				raw, _ := json.Marshal(untrusted.Value(result))
				note += clip(string(raw), planReadRunes)
			}
			ask = append(ask, pluginapi.ChatMessage{Role: "assistant", Content: content},
				pluginapi.ChatMessage{Role: "user", Content: note + "\n\nRead more if you need to, or reply with the parts as JSON."})
			continue
		}
		parts := swarm.ParseParts(content, maxParts)
		env.Emit(EventPlanner, map[string]any{"parts": len(parts), "role": role, "reads": reads})
		if len(parts) == 0 {
			return whole
		}
		return swarm.MergeOverlapping(parts)
	}
}

// pickGuidance asks the judge to pick a race's best change in a task.
const pickGuidance = "You are the judge. Several agents each did the same task on their own, in their own copy of the project, and each one's change is below as a diff with what the agent said about it. " +
	"Pick the change that does the task best: correct, complete, and no bigger than it needs to be. " +
	`Reply with JSON only: {"winner": "A", "why": "..."}.`

// pickDiffRunes caps each change as the judge reads it.
const pickDiffRunes = 12000

// taskJudge is the judge of a race on a task: it reads each change and picks
// one, since changes to code are kept whole rather than written together.
func taskJudge(env pluginapi.ExecutionEnvironment, profile contracts.AIProfile, role string) swarm.Judge {
	return func(ctx context.Context, request string, candidates []swarm.Candidate) int {
		judgeRole := judgeRoleFor(profile, role)
		announceRole(env, judgeRole)
		var b strings.Builder
		fmt.Fprintf(&b, "The task:\n%s\n", request)
		for i, c := range candidates {
			fmt.Fprintf(&b, "\nChange %s (agent %d):\nWhat the agent said: %s\n```diff\n%s\n```\n", draftLetter(i), c.Agent,
				clip(strings.TrimSpace(c.Answer), promptDraftRunes/4), clip(c.Diff, pickDiffRunes))
		}
		ask := []pluginapi.ChatMessage{{Role: "system", Content: pickGuidance}, {Role: "user", Content: b.String()}}
		text, _, err := generateText(pluginapi.WithGenerateOptions(ctx, pluginapi.GenerateOptions{MaxTokens: judgeTokens}), env, judgeRole, ask)
		if err != nil {
			return -1
		}
		return pickedChange(text, len(candidates))
	}
}

// pickedChange reads the judge's pick: a letter, or a number from 1.
func pickedChange(text string, n int) int {
	found, ok := structured.Extract(tools.VisibleText(text))
	if !ok {
		return -1
	}
	m, _ := found.Value.(map[string]any)
	switch w := m["winner"].(type) {
	case string:
		w = strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(w), "Change ")))
		if len(w) == 1 && w[0] >= 'A' && int(w[0]-'A') < n {
			return int(w[0] - 'A')
		}
	case float64:
		if i := int(w) - 1; i >= 0 && i < n {
			return i
		}
	}
	return -1
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
	// task is how a task swarm's merge and delivery went, and footer what
	// goes under the answer about it.
	task   *swarm.TaskOutcome
	footer string
}

// runSwarm plans a swarm, has the environment run its agents, and merges a
// race's answers. A split's answers come back as reference material. folder
// is a task's: the environment runs its agents as tasks there, merges their
// changes, and applies them.
func runSwarm(ctx context.Context, env pluginapi.ExecutionEnvironment, runner swarm.Runner, ch chan<- pluginapi.OrchestrationEvent,
	profile contracts.AIProfile, role, mode, prompt, instructions, folder string, named int) swarmTurn {
	b := swarmBudget(ctx, profile, named, folder != "")
	req := swarm.Request{Mode: mode, Prompt: prompt, Budget: b, Folder: folder}
	if folder != "" {
		req.Judge = taskJudge(env, profile, role)
	}
	if mode == swarm.ModeRace {
		req.Models = swarm.RaceModels(profile.Orchestration.Swarm, swarm.PolicyFrom(ctx), b.Agents)
		req.Parts = swarm.RaceParts(prompt, len(req.Models))
	} else {
		if folder != "" {
			req.Parts = askTaskPlanner(ctx, env, ch, profile, plannerRole(profile, role), prompt, folder, b.Agents)
		} else {
			req.Parts = askSwarmPlanner(ctx, env, ch, plannerRole(profile, role), prompt, b.Agents)
		}
		// Named models take the parts in turn, so a split on one computer
		// with two models loaded still runs its agents at once.
		if models := swarm.RaceModels(profile.Orchestration.Swarm, swarm.PolicyFrom(ctx), b.Agents); len(models) > 0 {
			for i := range req.Parts {
				req.Models = append(req.Models, models[i%len(models)])
			}
		}
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
	if out.Task != nil {
		// The environment merged, checked, and applied the agents' changes;
		// their reports are for the answer, whatever came of them.
		turn.task, turn.winner = out.Task, out.Task.Winner
		turn.ran = ctx.Err() == nil
		turn.footer = out.Task.Footer()
		var b strings.Builder
		b.WriteString("Reports from each agent:")
		for i, a := range out.Agents {
			if i >= len(out.Results) {
				break
			}
			r := out.Results[i]
			answer := strings.TrimSpace(r.Answer)
			if answer == "" {
				answer = "(no report)"
			}
			fmt.Fprintf(&b, "\n\nAgent %d, %s (%s):\n%s", a.N, a.Part.Title, r.Status, clip(answer, swarmAnswerRunes))
			if files, ok := out.Task.AgentFiles[a.N]; ok {
				changed := "none"
				if len(files) > 0 {
					changed = strings.Join(files, ", ")
				}
				fmt.Fprintf(&b, "\nFiles it really changed, from Git: %s", changed)
			}
		}
		if out.Task.Git {
			changed := "none"
			if len(out.Task.Files) > 0 {
				changed = strings.Join(out.Task.Files, ", ")
			}
			fmt.Fprintf(&b, "\n\nFiles in the change Toskar delivered: %s", changed)
		}
		turn.reference = b.String()
		return turn
	}
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
