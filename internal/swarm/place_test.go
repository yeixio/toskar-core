package swarm

import "testing"

func agentsOn(places ...[2]string) []Agent {
	out := make([]Agent, len(places))
	for i, p := range places {
		out[i] = Agent{N: i + 1, Role: AgentRole(i + 1), NodeID: p[0], ModelID: p[1]}
	}
	return out
}

// Agents on different computers, or on different models, run at once; agents
// that share a computer and a model wait their turn there.
func TestGroup(t *testing.T) {
	agents := agentsOn([2]string{"pc-1", "qwen"}, [2]string{"pc-2", "qwen"}, [2]string{"pc-1", "qwen"}, [2]string{"pc-1", "gemma"})
	lanes := Group(agents)
	if len(lanes) != 3 {
		t.Fatalf("lanes = %+v, want 3", lanes)
	}
	if lanes[0].NodeID != "pc-1" || lanes[0].ModelID != "qwen" || len(lanes[0].Agents) != 2 || lanes[0].Agents[1] != 2 {
		t.Errorf("first lane = %+v, want agents 0 and 2 on pc-1's qwen", lanes[0])
	}
	if got := AtOnce(lanes); got != 3 {
		t.Errorf("AtOnce = %d, want 3", got)
	}
	if Queued(lanes, 0) || !Queued(lanes, 2) || Queued(lanes, 3) {
		t.Error("only the second agent on pc-1's qwen waits")
	}
	if code := Single(ModeSplit, agents, lanes); code != "" {
		t.Errorf("Single = %q, want agents at once", code)
	}
}

// One computer and one model with one slot runs as one agent, and says why
// (docs/features/swarms.md, Decision 4).
func TestSingle(t *testing.T) {
	same := agentsOn([2]string{"pc-1", "qwen"}, [2]string{"pc-1", "qwen"}, [2]string{"pc-1", "qwen"})
	if code := Single(ModeSplit, same, Group(same)); code != OneAtATime {
		t.Errorf("one computer, one model: %q", code)
	}
	if Reason(OneAtATime) != "One agent: this computer can't run more than one at a time with this model." {
		t.Errorf("the one-line reason changed: %q", Reason(OneAtATime))
	}
	one := agentsOn([2]string{"pc-1", "qwen"})
	if code := Single(ModeSplit, one, Group(one)); code != OnePart {
		t.Errorf("one part: %q", code)
	}
	if code := Single(ModeRace, one, Group(one)); code != OneModel {
		t.Errorf("a race of one: %q", code)
	}
	// Two models on one computer race at once.
	two := agentsOn([2]string{"pc-1", "qwen"}, [2]string{"pc-1", "gemma"})
	if code := Single(ModeRace, two, Group(two)); code != "" {
		t.Errorf("two models: %q", code)
	}
	for _, code := range []string{OneAtATime, OnePart, OneModel, TaskLater, NoChat} {
		if Reason(code) == "" {
			t.Errorf("no reason for %q", code)
		}
	}
}

// The slots setting comes later; SlotsFor is where it plugs in, and two
// slots let two agents share a model.
func TestSlotsSeam(t *testing.T) {
	if SlotsFor("pc-1", "qwen") != 1 {
		t.Fatal("slots are 1 until the setting comes")
	}
	old := SlotsFor
	t.Cleanup(func() { SlotsFor = old })
	SlotsFor = func(string, string) int { return 2 }
	same := agentsOn([2]string{"pc-1", "qwen"}, [2]string{"pc-1", "qwen"}, [2]string{"pc-1", "qwen"})
	lanes := Group(same)
	if code := Single(ModeSplit, same, lanes); code != "" || AtOnce(lanes) != 2 || !Queued(lanes, 2) {
		t.Errorf("two slots: single %q, at once %d", code, AtOnce(lanes))
	}
}
