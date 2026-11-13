package swarm

import (
	"context"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Request is a planned swarm, for the chat environment to place and run.
type Request struct {
	Mode   string
	Prompt string
	// Parts are the agents' parts, one agent each: a split's plan, or in a
	// race the whole request once per model.
	Parts []Part
	// Models are a race's models, one per part. Empty gives each agent its
	// role's model.
	Models []string
	Budget Budget
}

// Outcome is how a swarm's agents went.
type Outcome struct {
	// ID is the swarm's record.
	ID string
	// Single, when set, is why the swarm runs as one agent instead (a code
	// for Reason): no agent ran, and the turn answers as an ordinary one.
	Single  string
	Agents  []Agent
	Results []Result
}

// Done are the indexes of the agents that finished with an answer.
func (o Outcome) Done() []int {
	var done []int
	for i, r := range o.Results {
		if r.Status == AgentDone && r.Answer != "" {
			done = append(done, i)
		}
	}
	return done
}

// Runner is a chat environment that runs swarms: it keeps the record, gives
// each agent a chat in the coordinator chat's project, places the agents,
// and runs their turns as ordinary chat turns.
type Runner interface {
	RunSwarm(ctx context.Context, req Request) (Outcome, error)
	// FinishSwarm records how the merge went: done, stopped, or failed, and
	// a race's winning agent, or 0.
	FinishSwarm(ctx context.Context, id, status string, winner int)
}

// Start is a swarm asked for through POST /api/v1/swarms.
type Start struct {
	Prompt string
	// Mode is split or race; empty is the profile's, or split.
	Mode string
	// ProjectID is one of the person's projects for a new chat to start
	// in; the caller checks it's theirs.
	ProjectID string
	// ConversationID is the person's chat to run it in; empty starts one.
	ConversationID string
	// Budget overrides the profile's swarm controls for this swarm.
	Budget *contracts.SwarmPolicy
}
