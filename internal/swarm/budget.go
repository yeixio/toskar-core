package swarm

import (
	"time"

	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Defaults (docs/features/swarms.md#budgets).
const (
	DefaultAgents          = 4
	DefaultTokens          = 400_000
	DefaultQuestionMinutes = 5
	DefaultTaskMinutes     = 30
)

// Budget is what a swarm may use. When it runs out, the agents still running
// are stopped, and the merge uses what finished.
type Budget struct {
	// Agents is the most agents the swarm has.
	Agents int
	// Tokens is the most the whole swarm may use, every agent together.
	Tokens int
	// Time is the most the agents may run.
	Time time.Duration
}

// Contract is the budget as the API shows it.
func (b Budget) Contract() contracts.SwarmBudget {
	return contracts.SwarmBudget{Agents: b.Agents, Tokens: b.Tokens, Minutes: int(b.Time / time.Minute)}
}

// BudgetFor is a swarm's budget: the turn's own controls over the profile's,
// over the defaults. maxWorkers is the profile's max_workers, the default
// cap on agents; task picks a task's time over a question's.
func BudgetFor(profile, turn *contracts.SwarmPolicy, maxWorkers int, task bool) Budget {
	b := Budget{Agents: DefaultAgents, Tokens: DefaultTokens, Time: DefaultQuestionMinutes * time.Minute}
	if task {
		b.Time = DefaultTaskMinutes * time.Minute
	}
	if maxWorkers >= profiles.SwarmMinAgents && maxWorkers <= profiles.SwarmMaxAgents {
		b.Agents = maxWorkers
	}
	for _, p := range []*contracts.SwarmPolicy{profile, turn} {
		if p == nil {
			continue
		}
		if p.Agents != 0 {
			b.Agents = clamp(p.Agents, profiles.SwarmMinAgents, profiles.SwarmMaxAgents)
		}
		if p.Tokens != 0 {
			b.Tokens = clamp(p.Tokens, profiles.SwarmMinTokens, profiles.SwarmMaxTokens)
		}
		if p.Minutes != 0 {
			b.Time = time.Duration(clamp(p.Minutes, profiles.SwarmMinMinutes, profiles.SwarmMaxMinutes)) * time.Minute
		}
	}
	return b
}

// ModeFor is the mode a turn's swarm runs in: the one asked for, else the
// turn's or profile's controls, else split.
func ModeFor(asked string, profile, turn *contracts.SwarmPolicy) string {
	if ValidMode(asked) {
		return asked
	}
	for _, p := range []*contracts.SwarmPolicy{turn, profile} {
		if p != nil && ValidMode(p.Mode) {
			return p.Mode
		}
	}
	return ModeSplit
}

// RaceModels are a race's models, one agent each: the turn's list, else the
// profile's, capped at the budget's agents.
func RaceModels(profile, turn *contracts.SwarmPolicy, agents int) []string {
	var models []string
	for _, p := range []*contracts.SwarmPolicy{turn, profile} {
		if p != nil && len(p.Models) > 0 {
			models = p.Models
			break
		}
	}
	if agents > 0 && len(models) > agents {
		models = models[:agents]
	}
	return append([]string(nil), models...)
}

func clamp(v, lo, hi int) int {
	return max(lo, min(hi, v))
}
