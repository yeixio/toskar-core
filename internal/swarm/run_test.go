package swarm

import (
	"context"
	"sync"
	"testing"
	"time"
)

// span is when an agent's work ran.
type span struct{ from, to time.Time }

func (s span) overlaps(o span) bool { return s.from.Before(o.to) && o.from.Before(s.to) }

// Agents on two computers overlap in time; two agents on one computer and
// model with one slot don't, and the second starts when the first is done.
func TestRunLanes(t *testing.T) {
	agents := agentsOn([2]string{"pc-1", "qwen"}, [2]string{"pc-2", "qwen"}, [2]string{"pc-1", "qwen"})
	var mu sync.Mutex
	spans := make([]span, len(agents))
	var order []string
	results := Run(context.Background(), agents, Group(agents, nil), Budget{Tokens: 1000, Time: time.Minute},
		func(ctx context.Context, a Agent, used func(int)) Result {
			from := time.Now()
			time.Sleep(60 * time.Millisecond)
			used(10)
			mu.Lock()
			spans[a.N-1] = span{from, time.Now()}
			mu.Unlock()
			return Result{Answer: "answer " + a.Role, Tokens: 10}
		}, Hooks{
			Started:  func(i int, a Agent) { mu.Lock(); order = append(order, "start "+a.Role); mu.Unlock() },
			Finished: func(i int, a Agent, r Result) { mu.Lock(); order = append(order, "end "+a.Role); mu.Unlock() },
		})
	if !spans[0].overlaps(spans[1]) {
		t.Errorf("agents on two computers didn't overlap: %+v", spans)
	}
	if spans[0].overlaps(spans[2]) {
		t.Errorf("two agents on one computer and model overlapped: %+v", spans)
	}
	for i, r := range results {
		if r.Status != AgentDone || r.Answer == "" {
			t.Errorf("agent %d: %+v", i+1, r)
		}
	}
	first, third := -1, -1
	for k, ev := range order {
		switch ev {
		case "end agent:1":
			first = k
		case "start agent:3":
			third = k
		}
	}
	if third < first {
		t.Errorf("the queued agent started before the one ahead of it finished: %v", order)
	}
	if tokens, secs := Totals(results); tokens != 30 || secs <= 0 {
		t.Errorf("totals = %d tokens, %v s", tokens, secs)
	}
}

// waitForStop is an agent that works until it's stopped, having used tokens.
func waitForStop(tokens int) Work {
	return func(ctx context.Context, a Agent, used func(int)) Result {
		used(tokens)
		select {
		case <-ctx.Done():
			return Result{Status: AgentStopped, Answer: "partial"}
		case <-time.After(5 * time.Second):
			return Result{Answer: "too slow"}
		}
	}
}

// At its token budget a swarm stops the agents still running, and those
// still queued never start; all of them are out of budget.
func TestRunTokenBudget(t *testing.T) {
	agents := agentsOn([2]string{"pc-1", "qwen"}, [2]string{"pc-2", "qwen"}, [2]string{"pc-1", "qwen"})
	var mu sync.Mutex
	started, over := 0, ""
	results := Run(context.Background(), agents, Group(agents, nil), Budget{Tokens: 100, Time: time.Minute}, waitForStop(60), Hooks{
		Started: func(int, Agent) { mu.Lock(); started++; mu.Unlock() },
		Over:    func(what string) { over = what },
	})
	for i, r := range results {
		if r.Status != AgentOutOfBudget {
			t.Errorf("agent %d: %s, want out of budget", i+1, r.Status)
		}
	}
	if started != 2 || over != "tokens" {
		t.Errorf("started %d agents, over %q; want 2 and tokens", started, over)
	}
}

func TestRunTimeBudget(t *testing.T) {
	agents := agentsOn([2]string{"pc-1", "qwen"}, [2]string{"pc-2", "qwen"})
	started := time.Now()
	results := Run(context.Background(), agents, Group(agents, nil), Budget{Tokens: 1000, Time: 50 * time.Millisecond}, waitForStop(1), Hooks{})
	if time.Since(started) > 3*time.Second {
		t.Fatal("the time budget didn't stop the agents")
	}
	for i, r := range results {
		if r.Status != AgentOutOfBudget || r.Answer != "partial" {
			t.Errorf("agent %d: %+v", i+1, r)
		}
	}
}

// Stopping one agent's chat stops that agent; the others finish. Stopping
// the swarm stops them all, and that's not the budget.
func TestRunStop(t *testing.T) {
	agents := agentsOn([2]string{"pc-1", "qwen"}, [2]string{"pc-2", "qwen"}, [2]string{"pc-3", "qwen"})
	results := Run(context.Background(), agents, Group(agents, nil), Budget{Tokens: 1000, Time: time.Minute},
		func(ctx context.Context, a Agent, used func(int)) Result {
			if a.N == 2 {
				return Result{Status: AgentStopped, Answer: "half"}
			}
			return Result{Answer: "whole"}
		}, Hooks{})
	if results[0].Status != AgentDone || results[1].Status != AgentStopped || results[2].Status != AgentDone {
		t.Errorf("stopping one: %+v", results)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(30 * time.Millisecond); cancel() }()
	queued := agentsOn([2]string{"pc-1", "qwen"}, [2]string{"pc-1", "qwen"})
	results = Run(ctx, queued, Group(queued, nil), Budget{Tokens: 1000, Time: time.Minute}, waitForStop(1), Hooks{})
	for i, r := range results {
		if r.Status != AgentStopped {
			t.Errorf("stopping all, agent %d: %s", i+1, r.Status)
		}
	}
}

// A model with two slots runs two of its agents at once; the third waits for
// one of them to finish.
func TestRunTwoSlots(t *testing.T) {
	agents := agentsOn([2]string{"pc-1", "qwen"}, [2]string{"pc-1", "qwen"}, [2]string{"pc-1", "qwen"})
	lanes := Group(agents, func(string, string) int { return 2 })
	var mu sync.Mutex
	spans := make([]span, len(agents))
	running, most := 0, 0
	Run(context.Background(), agents, lanes, Budget{Tokens: 1000, Time: time.Minute},
		func(ctx context.Context, a Agent, used func(int)) Result {
			mu.Lock()
			running++
			most = max(most, running)
			mu.Unlock()
			from := time.Now()
			time.Sleep(60 * time.Millisecond)
			mu.Lock()
			running--
			spans[a.N-1] = span{from, time.Now()}
			mu.Unlock()
			return Result{Answer: "answer", Tokens: 1}
		}, Hooks{})
	if most != 2 {
		t.Errorf("%d agents ran at once, want 2", most)
	}
	if !spans[0].overlaps(spans[1]) {
		t.Errorf("the first two didn't run together: %+v", spans)
	}
	if spans[2].from.Before(spans[0].to) && spans[2].from.Before(spans[1].to) {
		t.Errorf("the third started before a slot was free: %+v", spans)
	}
}
