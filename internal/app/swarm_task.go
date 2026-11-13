package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/swarm"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Task swarms (#1135, docs/features/swarms.md#folders): in a Git folder each
// agent works in a worktree on a branch of its own; the coordinator merges
// the branches, has a fix-up agent settle a conflict, runs the project's
// checks, and applies the result to the person's folder once they allow it.
// A race keeps the change whose checks pass, then the judge's pick.

// swarmBranchDays is how long a task swarm's branches are kept.
const swarmBranchDays = 7

// taskWorkspace opens the person's folder for a task swarm and gives each
// agent a worktree and branch.
func (e *chatExecEnv) taskWorkspace(ctx context.Context, st *swarmState, agents []swarm.Agent) (*swarm.Workspace, error) {
	dir := filepath.Join(e.app.Config.Get().DataDir, "swarms")
	ws, err := swarm.Prepare(ctx, st.rec.Folder, dir, st.rec.ID)
	if err != nil {
		return nil, err
	}
	for i := range agents {
		ag := &agents[i]
		if ag.Worktree, ag.Folder, ag.Branch, err = ws.Add(ctx, swarm.AgentBranchName(ag.N)); err != nil {
			ws.Remove(context.WithoutCancel(ctx))
			return nil, err
		}
	}
	return ws, nil
}

// finishTask merges a task swarm's work, checks it, and delivers it. Agents
// that took turns in the folder already left their changes there.
func (e *chatExecEnv) finishTask(ctx context.Context, st *swarmState, req swarm.Request, ws *swarm.Workspace,
	agents []swarm.Agent, results []swarm.Result, started time.Time) *swarm.TaskOutcome {
	t := &swarm.TaskOutcome{Folder: req.Folder, Git: ws != nil}
	if ws == nil || ctx.Err() != nil {
		return t
	}
	t.AgentFiles = map[int][]string{}
	for _, ag := range agents {
		t.AgentFiles[ag.N] = ws.Files(ctx, ag.Branch)
	}
	var done []int
	for i, r := range results {
		if r.Status == swarm.AgentDone {
			done = append(done, i)
		}
	}
	if req.Mode == swarm.ModeRace {
		e.pickRace(ctx, st, req, ws, agents, results, done, t)
	} else {
		e.mergeSplit(ctx, st, req, ws, agents, results, done, started, t)
	}
	if t.Branch != "" && ctx.Err() == nil {
		e.deliver(ctx, st, ws, t)
	}
	st.mu.Lock()
	st.rec.Branch, st.rec.Checks, st.rec.Conflicts, st.rec.Applied = t.Branch, t.Checks, t.Conflicts, t.Applied
	st.mu.Unlock()
	e.app.saveSwarm(ctx, st)
	return t
}

// mergeSplit merges the agents' branches one at a time into a merged branch,
// as Decision 1 says, then runs the project's checks on it.
func (e *chatExecEnv) mergeSplit(ctx context.Context, st *swarmState, req swarm.Request, ws *swarm.Workspace,
	agents []swarm.Agent, results []swarm.Result, done []int, started time.Time, t *swarm.TaskOutcome) {
	top, folder, branch, err := ws.Add(ctx, swarm.MergedBranchName)
	if err != nil {
		e.app.Logger.Warn("swarm merge worktree", "swarm_id", st.rec.ID, "error", err)
		return
	}
	t.Branch = branch
	budget := newFixupBudget(req.Budget, started, results)
	var merged []swarm.Agent
	for _, i := range done {
		ag := agents[i]
		if ctx.Err() != nil {
			return
		}
		if len(t.AgentFiles[ag.N]) == 0 {
			// It changed nothing in the folder: nothing to merge.
			continue
		}
		e.Emit(swarm.EventMerging, map[string]any{"swarm_id": st.rec.ID, "mode": req.Mode, "agent": ag.N, "branch": ag.Branch})
		conflicted, err := ws.Merge(ctx, top, ag.Branch, "Merge "+swarm.Label(ag.N, ag.Part.Title))
		if err == nil && len(conflicted) == 0 {
			merged = append(merged, ag)
			continue
		}
		if len(conflicted) > 0 && e.fixup(ctx, st, req, ws, top, folder, merged, ag, conflicted, budget) {
			merged = append(merged, ag)
			continue
		}
		ws.Abort(ctx, top)
		c := contracts.SwarmConflict{Agent: ag.N, Branch: ag.Branch, Files: conflicted}
		if c.Files == nil {
			c.Files = []string{}
		}
		t.Conflicts = append(t.Conflicts, c)
		e.Emit(swarm.EventMerging, map[string]any{"swarm_id": st.rec.ID, "mode": req.Mode, "agent": ag.N, "branch": ag.Branch,
			"conflict": map[string]any{"agent": c.Agent, "branch": c.Branch, "files": c.Files}})
	}
	for _, ag := range merged {
		t.Merged = append(t.Merged, ag.N)
	}
	if len(merged) == 0 {
		return
	}
	if t.Check = swarm.CheckCommand(folder); t.Check != "" {
		t.Checks = e.runCheck(ctx, folder, t.Check)
	}
}

// fixupBudget is what's left of a swarm's budget once its agents are done,
// for fix-up agents.
type fixupBudget struct {
	mu       sync.Mutex
	deadline time.Time
	// tokens are the tokens left, or -1 for no cap.
	tokens int
}

func newFixupBudget(b swarm.Budget, started time.Time, results []swarm.Result) *fixupBudget {
	f := &fixupBudget{tokens: -1}
	if b.Time > 0 {
		f.deadline = started.Add(b.Time)
	}
	if b.Tokens > 0 {
		used, _ := swarm.Totals(results)
		f.tokens = b.Tokens - used
	}
	return f
}

// left reports whether any budget is left.
func (f *fixupBudget) left() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.deadline.IsZero() && !time.Now().Before(f.deadline) {
		return false
	}
	// -1 is no token cap.
	return f.tokens != 0
}

// use spends tokens, and reports whether the budget is out.
func (f *fixupBudget) use(n int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tokens < 0 {
		return false
	}
	f.tokens = max(0, f.tokens-n)
	return f.tokens == 0
}

// fixup runs a fix-up agent in the merge's worktree to settle a conflict
// between an agent's change and what was merged before it, and reports
// whether it did. It has what's left of the swarm's budget.
func (e *chatExecEnv) fixup(ctx context.Context, st *swarmState, req swarm.Request, ws *swarm.Workspace, top, folder string,
	merged []swarm.Agent, theirs swarm.Agent, conflicted []string, budget *fixupBudget) bool {
	if !budget.left() {
		return false
	}
	a := e.app
	// The agent merged before whose change touches the same files.
	ours := swarm.Agent{N: 0, Part: swarm.Part{Title: "the merged work", Instructions: "The other agents' changes, merged so far."}}
	for _, m := range merged {
		if files := ws.Files(ctx, m.Branch); slices.ContainsFunc(conflicted, func(f string) bool { return slices.Contains(files, f) }) {
			ours = m
			break
		}
	}
	if ours.N == 0 && len(merged) > 0 {
		ours = merged[0]
	}
	st.mu.Lock()
	n := len(st.rec.Agents) + 1
	st.mu.Unlock()
	title := fmt.Sprintf("fix-up for agent %d", theirs.N)
	if ours.N > 0 {
		title = fmt.Sprintf("fix-up for agents %d and %d", ours.N, theirs.N)
	}
	fix := swarm.Agent{N: n, Role: swarm.AgentRole(n), Part: swarm.Part{Title: title, Instructions: "Settle the merge conflict in " + strings.Join(conflicted, ", ") + "."},
		ModelID: theirs.ModelID, NodeID: theirs.NodeID, Folder: folder, Worktree: top}
	conv, err := a.Conversations.Create(ctx, swarm.Label(fix.N, title), e.profile.ID, fix.ModelID)
	if err != nil {
		return false
	}
	fix.ConversationID = conv.ID
	if st.rec.ProjectID != "" {
		_ = a.Conversations.SetProjectID(ctx, conv.ID, st.rec.ProjectID)
	}
	_ = a.Conversations.SetSwarm(ctx, conv.ID, st.rec.ID, fix.Role)
	st.mu.Lock()
	st.rec.Agents = append(st.rec.Agents, contracts.SwarmAgent{Agent: fix.N, ConversationID: conv.ID, Role: fix.Role,
		Part: fix.Part.Instructions, ModelID: fix.ModelID, NodeID: fix.NodeID, Status: swarm.AgentRunning, Paths: conflicted})
	k := len(st.rec.Agents) - 1
	st.mu.Unlock()
	a.saveSwarm(ctx, st)
	e.Emit(swarm.EventAgentStarted, map[string]any{"swarm_id": st.rec.ID, "agent": fix.N,
		"agent_conversation_id": conv.ID, "node_id": fix.NodeID, "model": fix.ModelID, "fixup": true})

	fixCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if !budget.deadline.IsZero() {
		var stop context.CancelFunc
		fixCtx, stop = context.WithDeadline(fixCtx, budget.deadline)
		defer stop()
	}
	r := e.runAgent(fixCtx, st.rec.ID, fix, swarm.FixupMessage(req.Prompt, ours, theirs, conflicted), func(n int) {
		if budget.use(n) {
			cancel()
		}
	})
	if r.Status != swarm.AgentDone && fixCtx.Err() != nil && ctx.Err() == nil {
		r.Status = swarm.AgentOutOfBudget
	}
	ok := r.Status == swarm.AgentDone && ctx.Err() == nil && ws.Resolved(context.WithoutCancel(ctx), top, theirs.Branch, conflicted)
	summary := clipSummary(r.Answer)
	st.mu.Lock()
	sa := &st.rec.Agents[k]
	sa.Status, sa.Summary, sa.Tokens, sa.Seconds = r.Status, summary, r.Tokens, r.Seconds
	st.rec.Tokens += r.Tokens
	st.mu.Unlock()
	a.saveSwarm(ctx, st)
	e.Emit(swarm.EventAgentFinished, map[string]any{"swarm_id": st.rec.ID, "agent": fix.N,
		"agent_conversation_id": conv.ID, "status": r.Status, "summary": summary, "tokens": r.Tokens, "seconds": r.Seconds,
		"fixup": true, "resolved": ok})
	return ok
}

// pickRace picks a race's change: one whose checks pass beats one whose
// checks fail, and the judge picks among the rest.
func (e *chatExecEnv) pickRace(ctx context.Context, st *swarmState, req swarm.Request, ws *swarm.Workspace,
	agents []swarm.Agent, results []swarm.Result, done []int, t *swarm.TaskOutcome) {
	type entry struct {
		i      int
		checks string
		diff   string
	}
	var all []entry
	for _, i := range done {
		ag := agents[i]
		diff, err := ws.Diff(ctx, ag.Branch)
		if err != nil || strings.TrimSpace(diff) == "" {
			continue
		}
		en := entry{i: i, diff: diff}
		if check := swarm.CheckCommand(ag.Folder); check != "" {
			t.Check = check
			en.checks = e.runCheck(ctx, ag.Folder, check)
			st.mu.Lock()
			st.rec.Agents[i].Checks = en.checks
			st.mu.Unlock()
		}
		all = append(all, en)
	}
	if len(all) == 0 || ctx.Err() != nil {
		return
	}
	// Passing beats not checked, which beats failing.
	var pool []entry
	for _, want := range []string{swarm.ChecksPassed, "", swarm.ChecksFailed} {
		for _, en := range all {
			if en.checks == want {
				pool = append(pool, en)
			}
		}
		if len(pool) > 0 {
			break
		}
	}
	pick := 0
	if len(pool) > 1 && req.Judge != nil {
		candidates := make([]swarm.Candidate, len(pool))
		for k, en := range pool {
			ag := agents[en.i]
			candidates[k] = swarm.Candidate{Agent: ag.N, Title: ag.Part.Title, Diff: en.diff, Answer: results[en.i].Answer}
		}
		if k := req.Judge(ctx, req.Prompt, candidates); k >= 0 && k < len(pool) {
			pick = k
		}
	}
	won := pool[pick]
	t.Winner, t.Branch, t.Checks = agents[won.i].N, agents[won.i].Branch, won.checks
	t.Merged = []int{agents[won.i].N}
}

// runCheck runs the project's check command in dir, through the terminal tool
// as a task's command runs, and says how it went: passed, failed (or timed
// out), or "" when it couldn't run, such as when the profile doesn't allow the terminal.
func (e *chatExecEnv) runCheck(ctx context.Context, dir, command string) string {
	checkCtx := turnopts.WithProject(turnopts.WithLongCommands(ctx), dir)
	result, err := e.ExecuteTool(checkCtx, "terminal", map[string]any{"command": command})
	if err != nil {
		// Checks that ran past the time limit didn't pass.
		if tools.ErrorKind(err) == tools.ErrKindTimeout {
			return swarm.ChecksFailed
		}
		return ""
	}
	switch code := result["exit"].(type) {
	case int:
		if code != 0 {
			return swarm.ChecksFailed
		}
	case float64:
		if code != 0 {
			return swarm.ChecksFailed
		}
	}
	return swarm.ChecksPassed
}

// deliver applies the swarm's change to the person's folder as uncommitted
// changes, after the same approval a task's writes have: the person allows
// it, unless their profile allows writes outside tasks. A change whose checks
// failed always asks, and says so. A folder that changed since the swarm
// started, so the change no longer applies, isn't forced.
func (e *chatExecEnv) deliver(ctx context.Context, st *swarmState, ws *swarm.Workspace, t *swarm.TaskOutcome) {
	t.Files = ws.Files(ctx, t.Branch)
	if len(t.Files) == 0 {
		return
	}
	patch, err := ws.Diff(ctx, t.Branch)
	if err != nil {
		t.NotApplied = swarm.NotAppliedFailed
		return
	}
	if !ws.CanApply(ctx, patch) {
		t.NotApplied = swarm.NotAppliedChanged
		return
	}
	failed := t.Checks == swarm.ChecksFailed
	policy := tools.PolicyAsk
	switch {
	case failed:
		policy = tools.PolicyAskEachTime
	case tools.PolicyForProfile(e.profile, "filesystem.write") == tools.PolicyAllow:
		policy = tools.PolicyAllow
	}
	files := fmt.Sprintf("%d files", len(t.Files))
	if len(t.Files) == 1 {
		files = "1 file"
	}
	meta := map[string]any{"swarm_id": st.rec.ID}
	if e.conversationID != "" {
		meta["conversation_id"] = e.conversationID
	}
	if e.taskID != "" {
		meta["task_id"] = e.taskID
	}
	args := map[string]any{"path": t.Folder, "files": len(t.Files), "branch": t.Branch}
	reason := fmt.Sprintf("Apply the swarm's changes to %s: %s", t.Folder, files)
	if failed {
		reason += fmt.Sprintf(". The project's checks failed on them: %s.", t.Check)
	}
	if err := e.app.Tools.Confirm(ctx, swarm.ApplyAction, args, policy, reason, meta); err != nil {
		t.NotApplied = swarm.NotAppliedDeclined
		return
	}
	switch err := ws.Apply(context.WithoutCancel(ctx), patch); {
	case errors.Is(err, swarm.ErrApplyConflict):
		// Changed while the person was deciding.
		t.NotApplied = swarm.NotAppliedChanged
	case err != nil:
		t.NotApplied = swarm.NotAppliedFailed
	default:
		t.Applied = true
	}
}

// pruneSwarmBranches deletes task swarms' branches kept longer than
// swarmBranchDays, and forgets them in their records.
func (a *App) pruneSwarmBranches(ctx context.Context) {
	if a.Swarms == nil {
		return
	}
	old, err := a.Swarms.KeptBranches(ctx, time.Now().Add(-swarmBranchDays*24*time.Hour))
	if err != nil {
		a.Logger.Warn("swarm branches", "error", err)
		return
	}
	for _, s := range old {
		if _, err := swarm.DeleteBranches(ctx, s.Folder, s.ID); err != nil {
			a.Logger.Warn("delete swarm branches", "swarm_id", s.ID, "error", err)
			continue
		}
		_ = a.Swarms.ForgetBranches(ctx, s.ID)
	}
}

// swarmBranchesLoop prunes old swarm branches at start, then daily.
func (a *App) swarmBranchesLoop(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		a.pruneSwarmBranches(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
