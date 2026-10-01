// Package share decides who gets this computer when several kinds of work
// want it at once (spec §60). Interactive chat comes first, then scheduled
// automations, then knowledge indexing, then benchmarks, then training. Lower-priority work waits for
// higher-priority work to finish instead of competing with it for memory.
package share

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Class is a kind of work, in priority order.
type Class int

const (
	// Interactive is a chat or API request someone is waiting on.
	Interactive Class = iota
	// Automation is a scheduled automation run.
	Automation
	// Indexing is background embedding of knowledge passages for semantic
	// search (§61). It runs in small batches, so it yields quickly.
	Indexing
	// Benchmark is a performance benchmark.
	Benchmark
	// Training is a training run, which frees memory by unloading models.
	Training
	classes
)

func (c Class) String() string {
	switch c {
	case Interactive:
		return "chat"
	case Automation:
		return "automation"
	case Indexing:
		return "knowledge indexing"
	case Benchmark:
		return "benchmark"
	case Training:
		return "training"
	}
	return "work"
}

// waitingFor is how a wait is explained to the person.
func (c Class) waitingFor() string {
	switch c {
	case Interactive:
		return "Waiting for your chat to finish"
	case Automation:
		return "Waiting for an automation to finish"
	case Indexing:
		return "Waiting for knowledge indexing to finish"
	case Benchmark:
		return "Waiting for a benchmark to finish"
	}
	return "Waiting for other work to finish"
}

// DefaultGrace is how long chat still counts as active after a reply, so
// lower-priority work does not load a model between someone's messages.
const DefaultGrace = 20 * time.Second

// Gate admits work by priority.
type Gate struct {
	mu        sync.Mutex
	active    [classes]int
	lastEnded [classes]time.Time
	changed   chan struct{}
	works     map[*Work]struct{}
	grace     time.Duration
	now       func() time.Time
}

// New returns a gate. A grace of zero uses DefaultGrace.
func New(grace time.Duration) *Gate {
	if grace <= 0 {
		grace = DefaultGrace
	}
	return &Gate{changed: make(chan struct{}), works: map[*Work]struct{}{}, grace: grace, now: time.Now}
}

// Work is admitted work. Call Done when it ends.
type Work struct {
	g     *Gate
	class Class
	label string
	// remaining is the estimate in seconds, or -1 when unknown.
	remaining atomic.Int64
	done      atomic.Bool
}

// Label names the work for messages, such as a training run's AI.
func (w *Work) Label() string { return w.label }

// SetRemaining records how long the work expects to take from now.
func (w *Work) SetRemaining(d time.Duration) {
	if d < 0 {
		w.remaining.Store(-1)
		return
	}
	w.remaining.Store(int64(d / time.Second))
}

// Remaining is the latest estimate, if there is one.
func (w *Work) Remaining() (time.Duration, bool) {
	s := w.remaining.Load()
	if s < 0 {
		return 0, false
	}
	return time.Duration(s) * time.Second, true
}

// Done ends the work and lets waiting work in. It is safe to call twice.
func (w *Work) Done() {
	if w == nil || !w.done.CompareAndSwap(false, true) {
		return
	}
	g := w.g
	g.mu.Lock()
	g.active[w.class]--
	g.lastEnded[w.class] = g.now()
	delete(g.works, w)
	g.notifyLocked()
	g.mu.Unlock()
}

func (g *Gate) notifyLocked() {
	close(g.changed)
	g.changed = make(chan struct{})
}

// blockerLocked returns the higher-priority class that c must wait for, and
// when a grace period ends if that is all that is in the way.
func (g *Gate) blockerLocked(c Class) (Class, time.Time, bool) {
	now := g.now()
	for higher := Interactive; higher < c; higher++ {
		if g.active[higher] > 0 {
			return higher, time.Time{}, true
		}
	}
	// Chat that just finished probably continues; give it a moment.
	if c > Interactive && !g.lastEnded[Interactive].IsZero() {
		if until := g.lastEnded[Interactive].Add(g.grace); now.Before(until) {
			return Interactive, until, true
		}
	}
	return 0, time.Time{}, false
}

// Enter admits work of class c. Interactive work is admitted at once. Other
// work waits while work of a higher priority is running, and calls waiting
// with a plain explanation each time what it waits for changes. It returns
// ctx's error if ctx ends first.
func (g *Gate) Enter(ctx context.Context, c Class, label string, waiting func(reason string)) (*Work, error) {
	last := ""
	for {
		g.mu.Lock()
		blocker, until, blocked := Class(0), time.Time{}, false
		if c != Interactive {
			blocker, until, blocked = g.blockerLocked(c)
		}
		if !blocked {
			w := &Work{g: g, class: c, label: label}
			w.remaining.Store(-1)
			g.active[c]++
			g.works[w] = struct{}{}
			g.notifyLocked()
			g.mu.Unlock()
			return w, nil
		}
		changed := g.changed
		g.mu.Unlock()

		if reason := blocker.waitingFor(); reason != last && waiting != nil {
			last = reason
			waiting(reason)
		}
		var timer *time.Timer
		var expired <-chan time.Time
		if !until.IsZero() {
			timer = time.NewTimer(time.Until(until) + 10*time.Millisecond)
			expired = timer.C
		}
		select {
		case <-ctx.Done():
		case <-changed:
		case <-expired:
		}
		if timer != nil {
			timer.Stop()
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
}

// Running returns the first running work of class c, such as a training run
// a chat should mention.
func (g *Gate) Running(c Class) (*Work, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for w := range g.works {
		if w.class == c {
			return w, true
		}
	}
	return nil, false
}
