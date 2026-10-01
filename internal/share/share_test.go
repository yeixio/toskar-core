package share

import (
	"context"
	"testing"
	"time"
)

func enter(t *testing.T, g *Gate, c Class) *Work {
	t.Helper()
	w, err := g.Enter(context.Background(), c, c.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// admitted reports whether work of class c is admitted within d.
func admitted(g *Gate, c Class, d time.Duration) (*Work, []string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	var reasons []string
	w, err := g.Enter(ctx, c, c.String(), func(r string) { reasons = append(reasons, r) })
	return w, reasons, err == nil
}

func TestChatIsNeverKeptWaiting(t *testing.T) {
	g := New(time.Hour)
	enter(t, g, Training)
	enter(t, g, Automation)
	if _, _, ok := admitted(g, Interactive, 50*time.Millisecond); !ok {
		t.Fatal("chat waited")
	}
}

func TestLowerPriorityWaitsForChatAndItsGrace(t *testing.T) {
	g := New(80 * time.Millisecond)
	chat := enter(t, g, Interactive)
	if _, reasons, ok := admitted(g, Automation, 30*time.Millisecond); ok {
		t.Fatal("automation ran during chat")
	} else if len(reasons) != 1 || reasons[0] != "Waiting for your chat to finish" {
		t.Fatalf("reasons = %v", reasons)
	}
	chat.Done()
	if _, _, ok := admitted(g, Automation, 30*time.Millisecond); ok {
		t.Fatal("automation ran within the grace after chat")
	}
	start := time.Now()
	w, _, ok := admitted(g, Automation, time.Second)
	if !ok {
		t.Fatal("automation never ran after the grace")
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("automation waited too long")
	}
	w.Done()
}

func TestOrderAutomationBenchmarkTraining(t *testing.T) {
	g := New(time.Millisecond)
	auto := enter(t, g, Automation)
	if _, reasons, ok := admitted(g, Benchmark, 20*time.Millisecond); ok || reasons[0] != "Waiting for an automation to finish" {
		t.Fatalf("benchmark ran during an automation (%v)", reasons)
	}
	if _, _, ok := admitted(g, Training, 20*time.Millisecond); ok {
		t.Fatal("training ran during an automation")
	}
	auto.Done()
	bench, _, ok := admitted(g, Benchmark, time.Second)
	if !ok {
		t.Fatal("benchmark never ran")
	}
	if _, _, ok := admitted(g, Training, 20*time.Millisecond); ok {
		t.Fatal("training ran during a benchmark")
	}
	bench.Done()
	bench.Done() // twice is harmless
	if _, _, ok := admitted(g, Training, time.Second); !ok {
		t.Fatal("training never ran")
	}
}

func TestWaitingWorkEntersWhenTheBlockerEnds(t *testing.T) {
	g := New(time.Millisecond)
	auto := enter(t, g, Automation)
	got := make(chan *Work, 1)
	go func() {
		w, _ := g.Enter(context.Background(), Training, "Tire shop", nil)
		got <- w
	}()
	time.Sleep(20 * time.Millisecond)
	auto.Done()
	select {
	case w := <-got:
		if run, ok := g.Running(Training); !ok || run != w || run.Label() != "Tire shop" {
			t.Fatal("training not reported as running")
		}
		if _, ok := w.Remaining(); ok {
			t.Fatal("estimate before one was set")
		}
		w.SetRemaining(90 * time.Second)
		if d, ok := w.Remaining(); !ok || d != 90*time.Second {
			t.Fatalf("remaining = %v %v", d, ok)
		}
	case <-time.After(time.Second):
		t.Fatal("training stayed waiting")
	}
}
