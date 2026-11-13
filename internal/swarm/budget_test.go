package swarm

import (
	"testing"
	"time"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestBudgetFor(t *testing.T) {
	b := BudgetFor(nil, nil, 0, false)
	if b.Agents != 4 || b.Tokens != 400_000 || b.Time != 5*time.Minute {
		t.Errorf("defaults = %+v", b)
	}
	if b := BudgetFor(nil, nil, 0, true); b.Time != 30*time.Minute {
		t.Errorf("a task's time = %v", b.Time)
	}
	if b := BudgetFor(nil, nil, 6, false); b.Agents != 6 {
		t.Errorf("max_workers caps the agents: %d", b.Agents)
	}
	profile := &contracts.SwarmPolicy{Agents: 3, Tokens: 50_000, Minutes: 10}
	turn := &contracts.SwarmPolicy{Tokens: 20_000}
	b = BudgetFor(profile, turn, 6, false)
	if b.Agents != 3 || b.Tokens != 20_000 || b.Time != 10*time.Minute {
		t.Errorf("the turn's over the profile's over max_workers: %+v", b)
	}
	b = BudgetFor(&contracts.SwarmPolicy{Agents: 40, Tokens: 5, Minutes: 900}, nil, 0, false)
	if b.Agents != 8 || b.Tokens != 10_000 || b.Time != 240*time.Minute {
		t.Errorf("clamped: %+v", b)
	}
	if got := b.Contract(); got.Minutes != 240 || got.Agents != 8 {
		t.Errorf("contract = %+v", got)
	}
}

func TestModeAndModels(t *testing.T) {
	race := &contracts.SwarmPolicy{Mode: ModeRace, Models: []string{"a", "b", "c"}}
	if ModeFor("", nil, nil) != ModeSplit || ModeFor("", race, nil) != ModeRace || ModeFor(ModeSplit, race, nil) != ModeSplit || ModeFor("bogus", race, nil) != ModeRace {
		t.Error("the mode asked for, else the controls', else split")
	}
	if got := RaceModels(race, nil, 2); len(got) != 2 || got[1] != "b" {
		t.Errorf("RaceModels = %v, want a and b", got)
	}
	if got := RaceModels(race, &contracts.SwarmPolicy{Models: []string{"x", "y"}}, 4); got[0] != "x" {
		t.Errorf("the turn's models come first: %v", got)
	}
}
