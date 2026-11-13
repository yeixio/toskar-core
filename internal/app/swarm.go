package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/internal/locale"
	"github.com/yeixio/toskar-core/internal/projects"
	"github.com/yeixio/toskar-core/internal/runlog"
	"github.com/yeixio/toskar-core/internal/swarm"
	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Swarms (#1135, docs/features/swarms.md): the chat environment runs a
// swarm's agents. Each agent is an ordinary chat turn (RunChat) in a chat of
// its own, in the coordinator chat's project, marked with the swarm's id and
// its role, agent:N. Its tools ask as any chat's do, its approvals carry the
// swarm and the agent, and the person can open and continue its chat.

// ErrSwarmRole is a swarm asked for by someone who can't start one: Members
// and up can; Visitors and portals' guests can't.
var ErrSwarmRole = contracts.NewError("ROLE_REQUIRED", map[string]any{"role": "member"}, errors.New("starting a swarm needs the member role"))

// summaryRunes caps an agent's answer as its lane shows it.
const summaryRunes = 280

// maySwarm reports whether the person may start a swarm.
func maySwarm(ctx context.Context) bool {
	p := auth.PrincipalFrom(ctx).Person
	return p.PortalID == "" && p.Role.AtLeast(auth.RoleMember)
}

// swarmState is a running swarm's record, kept in step with the store.
type swarmState struct {
	mu  sync.Mutex
	rec contracts.Swarm
}

func (s *swarmState) snapshot() contracts.Swarm {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.rec
	out.Agents = append([]contracts.SwarmAgent(nil), s.rec.Agents...)
	return out
}

// save writes the record, whatever happened to the turn.
func (a *App) saveSwarm(ctx context.Context, st *swarmState) {
	if a.Swarms == nil {
		return
	}
	if err := a.Swarms.Save(context.WithoutCancel(ctx), st.snapshot()); err != nil && a.Logger != nil {
		a.Logger.Warn("save swarm", "swarm_id", st.rec.ID, "error", err)
	}
}

// swarmRecord is the turn's swarm record: the one POST /swarms made, or a
// new one.
func (e *chatExecEnv) swarmRecord(ctx context.Context, req swarm.Request) (*swarmState, error) {
	a := e.app
	if id := swarm.IDFrom(ctx); id != "" {
		rec, err := a.Swarms.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		rec.Mode = req.Mode
		return &swarmState{rec: rec}, nil
	}
	conv, err := a.Conversations.Get(ctx, e.conversationID)
	if err != nil {
		return nil, err
	}
	rec, err := a.Swarms.Create(ctx, conv.ProjectID, e.conversationID, req.Mode, req.Prompt, req.Budget.Contract())
	if err != nil {
		return nil, err
	}
	return &swarmState{rec: rec}, nil
}

// noteSingle says why a swarm runs as one agent: on the event stream, under
// the answer, and in the record.
func (e *chatExecEnv) noteSingle(st *swarmState, code string) {
	payload := map[string]any{"reason": code, "note": swarm.Reason(code)}
	if st != nil {
		payload["swarm_id"] = st.rec.ID
		st.mu.Lock()
		st.rec.Note = swarm.Reason(code)
		st.mu.Unlock()
	}
	e.Emit(swarm.EventNote, payload)
}

// RunSwarm places a planned swarm's agents and runs them, each in a chat of
// its own (swarm.Runner). Agents that can't run at once on any computer and
// model make it run as one agent, which the turn answers as.
func (e *chatExecEnv) RunSwarm(ctx context.Context, req swarm.Request) (swarm.Outcome, error) {
	a := e.app
	if !maySwarm(ctx) {
		return swarm.Outcome{}, ErrSwarmRole
	}
	if a.Swarms == nil {
		return swarm.Outcome{}, errors.New("swarms are not available")
	}
	if e.conversationID == "" {
		e.noteSingle(nil, swarm.NoChat)
		return swarm.Outcome{Single: swarm.NoChat}, nil
	}
	st, err := e.swarmRecord(ctx, req)
	if err != nil {
		return swarm.Outcome{}, err
	}
	e.mu.Lock()
	e.swarm = st
	e.mu.Unlock()
	out := swarm.Outcome{ID: st.rec.ID}
	_ = a.Conversations.SetSwarm(ctx, e.conversationID, st.rec.ID, "")

	// Place the agents one at a time, so each sees where the others went.
	agents := make([]swarm.Agent, len(req.Parts))
	for i, part := range req.Parts {
		n := i + 1
		role := swarm.AgentRole(n)
		if i < len(req.Models) {
			e.setAgentModel(role, req.Models[i])
		}
		node, err := e.NodeForRole(role)
		if err != nil {
			st.rec.Status, st.rec.Note = swarm.StatusFailed, err.Error()
			a.saveSwarm(ctx, st)
			return out, err
		}
		agents[i] = swarm.Agent{N: n, Role: role, Part: part, ModelID: e.modelForRole(role), NodeID: node}
	}
	lanes := swarm.Group(agents)
	code := swarm.Single(req.Mode, agents, lanes)
	// A task's folder outside Git has no copies to give the agents: they
	// take turns in it, which doesn't need them to run at once. A race
	// can't take turns at the same change.
	shared := false
	if req.Folder != "" {
		if _, isGit := swarm.RepoRoot(ctx, req.Folder); !isGit {
			switch {
			case req.Mode == swarm.ModeRace && (code == "" || code == swarm.OneAtATime):
				code = swarm.RaceNeedsGit
			case code == "" || code == swarm.OneAtATime:
				code, shared = "", true
			}
		}
	}
	if code != "" {
		e.noteSingle(st, code)
		a.saveSwarm(ctx, st)
		out.Single = code
		return out, nil
	}

	// The project: the coordinator chat's, or a new one named after the job
	// that the chat moves into.
	projectID, err := e.swarmProject(ctx, req.Prompt)
	if err != nil {
		return out, err
	}
	st.rec.ProjectID = projectID
	for i := range agents {
		ag := &agents[i]
		conv, err := a.Conversations.Create(ctx, swarm.Label(ag.N, ag.Part.Title), e.profile.ID, ag.ModelID)
		if err != nil {
			return out, err
		}
		ag.ConversationID = conv.ID
		if projectID != "" {
			if err := a.Conversations.SetProjectID(ctx, conv.ID, projectID); err != nil {
				return out, err
			}
		}
		if err := a.Conversations.SetSwarm(ctx, conv.ID, st.rec.ID, ag.Role); err != nil {
			return out, err
		}
	}
	// A task's agents work in its folder: each in a worktree of its own in
	// Git, or all in the folder, one after another.
	var ws *swarm.Workspace
	if req.Folder != "" {
		st.mu.Lock()
		st.rec.Folder = req.Folder
		st.mu.Unlock()
		if shared {
			lanes = swarm.OneAfterAnother(agents)
			for i := range agents {
				agents[i].Folder = req.Folder
			}
			e.noteSingle(st, swarm.NotGit)
		} else {
			if ws, err = e.taskWorkspace(ctx, st, agents); err != nil {
				st.mu.Lock()
				st.rec.Status, st.rec.Note = swarm.StatusFailed, err.Error()
				st.mu.Unlock()
				a.saveSwarm(ctx, st)
				return out, err
			}
			// The worktrees go when the swarm ends; the branches stay.
			defer ws.Remove(context.WithoutCancel(ctx))
		}
	}
	lanesOut := make([]map[string]any, len(agents))
	st.mu.Lock()
	st.rec.Agents = make([]contracts.SwarmAgent, len(agents))
	for i, ag := range agents {
		status := swarm.AgentRunning
		if swarm.Queued(lanes, i) {
			status = swarm.AgentQueued
		}
		st.rec.Agents[i] = contracts.SwarmAgent{Agent: ag.N, ConversationID: ag.ConversationID, Role: ag.Role,
			Part: ag.Part.Instructions, ModelID: ag.ModelID, NodeID: ag.NodeID, Status: status,
			Paths: ag.Part.Paths, Branch: ag.Branch}
		lane := map[string]any{"agent": ag.N, "conversation_id": ag.ConversationID, "part": ag.Part.Title,
			"role": ag.Role, "model": ag.ModelID, "node_id": ag.NodeID, "status": status}
		if ag.Branch != "" {
			lane["branch"] = ag.Branch
		}
		if len(ag.Part.Paths) > 0 {
			lane["paths"] = ag.Part.Paths
		}
		lanesOut[i] = lane
	}
	st.mu.Unlock()
	a.saveSwarm(ctx, st)
	e.Emit(swarm.EventStarted, map[string]any{"swarm_id": st.rec.ID, "mode": req.Mode, "agents": lanesOut,
		"budget": req.Budget.Contract()})

	setAgent := func(i int, f func(*contracts.SwarmAgent)) {
		st.mu.Lock()
		f(&st.rec.Agents[i])
		st.mu.Unlock()
		a.saveSwarm(ctx, st)
	}
	started := time.Now()
	results := swarm.Run(ctx, agents, lanes, req.Budget, func(runCtx context.Context, ag swarm.Agent, used func(int)) swarm.Result {
		message := swarm.FirstMessage(req.Mode, req.Prompt, req.Parts, ag.N)
		if req.Folder != "" {
			message = swarm.TaskMessage(req.Mode, req.Prompt, req.Parts, ag.N, shared)
		}
		r := e.runAgent(runCtx, st.rec.ID, ag, message, used)
		if ws != nil {
			// What it did, finished or not, is kept on its branch.
			if _, err := ws.Commit(context.WithoutCancel(ctx), ag.Worktree, swarm.Label(ag.N, ag.Part.Title)); err != nil && a.Logger != nil {
				a.Logger.Warn("commit a swarm agent's work", "swarm_id", st.rec.ID, "agent", ag.N, "error", err)
			}
		}
		return r
	}, swarm.Hooks{
		Started: func(i int, ag swarm.Agent) {
			setAgent(i, func(sa *contracts.SwarmAgent) { sa.Status = swarm.AgentRunning })
			e.Emit(swarm.EventAgentStarted, map[string]any{"swarm_id": st.rec.ID, "agent": ag.N,
				"agent_conversation_id": ag.ConversationID, "node_id": ag.NodeID, "model": ag.ModelID})
		},
		Finished: func(i int, ag swarm.Agent, r swarm.Result) {
			summary := clipSummary(r.Answer)
			setAgent(i, func(sa *contracts.SwarmAgent) {
				sa.Status, sa.Summary, sa.Tokens, sa.Seconds = r.Status, summary, r.Tokens, r.Seconds
			})
			e.Emit(swarm.EventAgentFinished, map[string]any{"swarm_id": st.rec.ID, "agent": ag.N,
				"agent_conversation_id": ag.ConversationID, "status": r.Status, "summary": summary,
				"tokens": r.Tokens, "seconds": r.Seconds})
		},
	})
	st.mu.Lock()
	st.rec.Tokens, st.rec.Seconds = swarm.Totals(results)
	st.rec.Status = swarm.StatusMerging
	st.mu.Unlock()
	a.saveSwarm(ctx, st)
	out.Agents, out.Results = agents, results
	if req.Folder != "" {
		out.Task = e.finishTask(ctx, st, req, ws, agents, results, started)
	}
	return out, nil
}

// FinishSwarm records how the merge went, and says so on the event stream.
func (e *chatExecEnv) FinishSwarm(ctx context.Context, id, status string, winner int) {
	e.mu.Lock()
	st := e.swarm
	e.mu.Unlock()
	if st == nil || st.rec.ID != id {
		return
	}
	st.mu.Lock()
	st.rec.Status, st.rec.Winner = status, winner
	tokens, seconds := st.rec.Tokens, st.rec.Seconds
	st.mu.Unlock()
	e.app.saveSwarm(ctx, st)
	payload := map[string]any{"swarm_id": id, "status": status, "tokens": tokens, "seconds": seconds}
	if winner > 0 {
		payload["winner"] = winner
	}
	e.Emit(swarm.EventFinished, payload)
}

// setAgentModel gives an agent's role its model, such as a race's.
func (e *chatExecEnv) setAgentModel(role, modelID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.agentModels == nil {
		e.agentModels = map[string]string{}
	}
	e.agentModels[role] = modelID
}

// agentModel is the model set for an agent's role, if any.
func (e *chatExecEnv) agentModel(role string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.agentModels[role]
}

// swarmProject is the project a swarm runs in: the coordinator chat's, or a
// new one named after the job, which the chat moves into. The chat's
// project line shows it, and the chat can be moved out.
func (e *chatExecEnv) swarmProject(ctx context.Context, request string) (string, error) {
	a := e.app
	conv, err := a.Conversations.Get(ctx, e.conversationID)
	if err != nil {
		return "", err
	}
	if conv.ProjectID != "" || a.Projects == nil {
		return conv.ProjectID, nil
	}
	name := swarm.JobName(request)
	p, err := a.Projects.Create(ctx, projects.Fields{Name: &name})
	if err != nil {
		return "", err
	}
	if err := a.Conversations.SetProjectID(ctx, conv.ID, p.ID); err != nil {
		return "", err
	}
	return p.ID, nil
}

// runAgent runs one agent's turn in its chat, as any chat turn runs, with
// message as its first message, and waits for its answer. Its tool calls show
// in the coordinator chat's lanes as they start. A task's agent works in its
// folder, as a task.
func (e *chatExecEnv) runAgent(ctx context.Context, swarmID string, ag swarm.Agent, message string, used func(int)) swarm.Result {
	a := e.app
	var mu sync.Mutex
	stopped, steps := false, 0
	note := func(evt events.Event) {
		if id, _ := evt.Payload["conversation_id"].(string); id != "" && id != ag.ConversationID {
			return
		}
		switch evt.Type {
		case events.ChatStopped:
			// The person stopped the agent's chat, or started another turn
			// in it: this agent stops, and the others go on.
			mu.Lock()
			stopped = true
			mu.Unlock()
		case events.ToolStarted:
			mu.Lock()
			steps++
			step := steps
			mu.Unlock()
			tool, _ := evt.Payload["tool_id"].(string)
			summary, _ := evt.Payload["summary"].(string)
			e.Emit(swarm.EventAgentProgress, map[string]any{"swarm_id": swarmID, "agent": ag.N, "step": step,
				"tool": tool, "summary": summary})
		}
	}
	ref := swarm.AgentRef{SwarmID: swarmID, N: ag.N, Label: swarm.Label(ag.N, ag.Part.Title), NodeID: ag.NodeID, ModelID: ag.ModelID, Task: ag.Folder != ""}
	agentCtx, cancel := agentContext(ctx, e.ctx, ref, ag.Folder, note)
	defer cancel()
	started := time.Now()
	ch, err := a.RunChat(agentCtx, e.profile.ID, ag.ConversationID, message, true, ag.ModelID, "")
	if err != nil {
		return swarm.Result{Status: swarm.AgentFailed, Error: err.Error(), Seconds: time.Since(started).Seconds()}
	}
	var answer strings.Builder
	estimated := 0
	var metrics *pluginapi.GenerationMetrics
	failure := ""
	for chunk := range ch {
		if chunk.Replace {
			answer.Reset()
		}
		if chunk.Content != "" {
			answer.WriteString(chunk.Content)
			n := tokensOf(chunk.Content)
			estimated += n
			used(n)
		}
		if chunk.Metrics != nil {
			metrics = chunk.Metrics
		}
		if chunk.Error != "" {
			failure = chunk.Error
		}
	}
	tokens := estimated
	if metrics != nil {
		if real := metrics.PromptTokens + metrics.CompletionTokens; real > estimated {
			used(real - estimated)
			tokens = real
		}
	}
	r := swarm.Result{Answer: strings.TrimSpace(answer.String()), Tokens: tokens, Seconds: time.Since(started).Seconds()}
	mu.Lock()
	wasStopped := stopped
	mu.Unlock()
	switch {
	case wasStopped || ctx.Err() != nil:
		r.Status = swarm.AgentStopped
	case failure != "":
		r.Status, r.Error = swarm.AgentFailed, failure
	case r.Answer == "":
		r.Status = swarm.AgentFailed
	default:
		r.Status = swarm.AgentDone
	}
	return r
}

// agentContext is an agent's turn's context: the coordinator's person, and
// what the request chose that still applies, such as tools an API key
// narrowed, without the coordinator's own chat, attachments, folder, or
// progress. A task's agent works in folder, its own. It stops when ctx does:
// the swarm stopped, or out of budget.
func agentContext(ctx, turn context.Context, ref swarm.AgentRef, folder string, note func(events.Event)) (context.Context, context.CancelFunc) {
	out := auth.WithPrincipal(context.Background(), auth.PrincipalFrom(turn))
	origin := runlog.OriginFrom(turn)
	// An agent of a question has no folder, wherever it was asked from.
	out = runlog.WithOrigin(out, runlog.Origin{Remote: origin.Remote})
	out = huginn.WithEffort(out, huginn.EffortFrom(turn))
	out = locale.WithTimeZone(out, locale.TimeZone(turn).String())
	if o := pluginapi.AnswerOptionsFrom(turn); o != (pluginapi.GenerateOptions{}) {
		out = pluginapi.WithAnswerOptions(out, o)
	}
	if o := turnopts.From(turn); o != nil {
		// What an API key or request narrowed stays narrowed, never wider.
		out = turnopts.With(out, &turnopts.Options{
			Memory: o.Memory, Knowledge: o.Knowledge, KnowledgeSources: o.KnowledgeSources,
			Tools: o.Tools, ReadOnlyTools: o.ReadOnlyTools, Language: o.Language, ExactModel: o.ExactModel,
		})
	}
	if folder != "" {
		// Only a task swarm's agent has one: a folder Core chose for it,
		// from the coordinator's, which an Admin's client named.
		out = turnopts.WithProject(out, folder)
	}
	out = swarm.WithAgent(out, ref)
	out = events.WithNote(out, note)
	out, cancel := context.WithCancel(out)
	stop := context.AfterFunc(ctx, cancel)
	return out, func() {
		stop()
		cancel()
	}
}

func clipSummary(s string) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > summaryRunes {
		return strings.TrimSpace(string([]rune(s)[:summaryRunes])) + "…"
	}
	return s
}

// swarmNotice is the line under the answer that says why a swarm ran as one
// agent, in the person's language.
func swarmNotice(lang, code string) string {
	text := locale.T(lang, "chat:notices.swarm."+code, nil)
	if text == "" || text == "chat:notices.swarm."+code {
		return swarm.Reason(code)
	}
	return text
}
