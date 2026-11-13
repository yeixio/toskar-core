package swarm

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Result is how one agent's turn went.
type Result struct {
	// Status is done, failed, stopped, or out_of_budget.
	Status string
	// Answer is what the agent wrote: whole when done, what it got to when
	// it was stopped.
	Answer  string
	Tokens  int
	Seconds float64
	// Error says why it failed.
	Error string
}

// Work runs agent a's turn. It calls used with the tokens the turn uses as it
// goes, so the swarm's budget is kept across every agent. Stopped from
// outside, by ctx or the person stopping the agent's chat, it returns
// stopped with what was written so far.
type Work func(ctx context.Context, a Agent, used func(tokens int)) Result

// Hooks hear about agents as they start and finish, for the swarm's events
// and record. Either may be nil. They may be called from several goroutines
// at once.
type Hooks struct {
	Started  func(i int, a Agent)
	Finished func(i int, a Agent, r Result)
	// Over is called once when the swarm runs out of tokens or time, with
	// "tokens" or "time".
	Over func(what string)
}

// Run runs a placed swarm's agents: each lane's agents in turn, up to its
// slots at once, and every lane at the same time. When the budget's tokens or
// time run out, the agents still running are stopped and those still queued
// don't start; they're out of budget. Stopping ctx stops them all. It returns
// each agent's result once every agent has finished.
func Run(ctx context.Context, agents []Agent, lanes []Lane, b Budget, work Work, hooks Hooks) []Result {
	results := make([]Result, len(agents))
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if b.Time > 0 {
		var stopTimer context.CancelFunc
		runCtx, stopTimer = context.WithTimeout(runCtx, b.Time)
		defer stopTimer()
	}
	var tokens atomic.Int64
	var over atomic.Value // "tokens" or "time"
	var overOnce sync.Once
	runOut := func(what string) {
		overOnce.Do(func() {
			over.Store(what)
			cancel()
			if hooks.Over != nil {
				hooks.Over(what)
			}
		})
	}
	used := func(n int) {
		if n <= 0 {
			return
		}
		if total := tokens.Add(int64(n)); b.Tokens > 0 && total >= int64(b.Tokens) {
			runOut("tokens")
		}
	}
	// outOfBudget reports a stop that came from the budget, not the person.
	outOfBudget := func() bool {
		if ctx.Err() != nil {
			return false
		}
		if over.Load() != nil {
			return true
		}
		if runCtx.Err() == context.DeadlineExceeded {
			runOut("time")
			return true
		}
		return false
	}
	finish := func(i int, r Result) {
		results[i] = r
		if hooks.Finished != nil {
			hooks.Finished(i, agents[i], r)
		}
	}
	runOne := func(i int) {
		if runCtx.Err() != nil {
			// Never started: the swarm was stopped or ran out first.
			status := AgentStopped
			if outOfBudget() {
				status = AgentOutOfBudget
			}
			finish(i, Result{Status: status})
			return
		}
		if hooks.Started != nil {
			hooks.Started(i, agents[i])
		}
		started := time.Now()
		r := work(runCtx, agents[i], used)
		if r.Seconds == 0 {
			r.Seconds = time.Since(started).Seconds()
		}
		if r.Status == "" {
			r.Status = AgentDone
		}
		if r.Status != AgentDone && runCtx.Err() != nil && outOfBudget() {
			r.Status = AgentOutOfBudget
		}
		finish(i, r)
	}
	var wg sync.WaitGroup
	for _, lane := range lanes {
		// Each of the lane's slots takes the lane's next agent, in order.
		var mu sync.Mutex
		next := 0
		take := func() (int, bool) {
			mu.Lock()
			defer mu.Unlock()
			if next >= len(lane.Agents) {
				return 0, false
			}
			next++
			return lane.Agents[next-1], true
		}
		for range min(max(1, lane.Slots), len(lane.Agents)) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i, ok := take(); ok; i, ok = take() {
					runOne(i)
				}
			}()
		}
	}
	wg.Wait()
	return results
}

// Totals are a swarm's tokens and seconds: every agent's tokens, and the time
// the slowest took.
func Totals(results []Result) (tokens int, seconds float64) {
	for _, r := range results {
		tokens += r.Tokens
		seconds = max(seconds, r.Seconds)
	}
	return tokens, seconds
}
