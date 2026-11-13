package swarm

// Agent is one agent of a swarm, as planned and placed.
type Agent struct {
	// N is its number, from 1.
	N    int
	Role string
	Part Part
	// ModelID and NodeID are where placement put it.
	ModelID string
	NodeID  string
	// ConversationID is its own chat.
	ConversationID string
}

// Lane is the agents that share one computer and model. A model on a
// computer answers as many requests at once as it has slots; the lane's
// other agents wait for one.
type Lane struct {
	NodeID  string
	ModelID string
	Slots   int
	// Agents are indexes into the swarm's agents, in order.
	Agents []int
}

// SlotsFor is how many requests a model on a computer answers at once.
// llama-server runs with --parallel 1 (#834), so it's 1 until the per-model
// slots setting comes (docs/features/swarms.md#placement); that change
// belongs here.
var SlotsFor = func(nodeID, modelID string) int { return 1 }

// Group puts placed agents into lanes by computer and model, in the order
// each lane's first agent was placed.
func Group(agents []Agent) []Lane {
	var lanes []Lane
	at := map[[2]string]int{}
	for i, a := range agents {
		key := [2]string{a.NodeID, a.ModelID}
		k, ok := at[key]
		if !ok {
			k = len(lanes)
			at[key] = k
			lanes = append(lanes, Lane{NodeID: a.NodeID, ModelID: a.ModelID, Slots: max(1, SlotsFor(a.NodeID, a.ModelID))})
		}
		lanes[k].Agents = append(lanes[k].Agents, i)
	}
	return lanes
}

// AtOnce is how many agents can run at the same time across the lanes.
func AtOnce(lanes []Lane) int {
	n := 0
	for _, l := range lanes {
		n += min(l.Slots, len(l.Agents))
	}
	return n
}

// Queued reports whether agent i waits for a slot when the swarm starts.
func Queued(lanes []Lane, i int) bool {
	for _, l := range lanes {
		for pos, j := range l.Agents {
			if j == i {
				return pos >= l.Slots
			}
		}
	}
	return false
}

// Why a swarm runs as one agent, by code. Each has a line in English for
// events and the record (Reason), and chat:notices.swarm.<code> for the
// person's language.
const (
	// OneAtATime: everything was placed on one computer and one model with
	// one slot, so nothing would run at once (docs/features/swarms.md,
	// Decision 4).
	OneAtATime = "oneAtATime"
	// OnePart: the coordinator couldn't split the request.
	OnePart = "onePart"
	// OneModel: a race needs at least two models to compare.
	OneModel = "oneModel"
	// TaskLater: a task in a folder, or from the terminal, runs as one task
	// for now.
	TaskLater = "taskLater"
	// NoChat: a swarm keeps its agents in chats, and the turn has none.
	NoChat = "noChat"
)

var reasons = map[string]string{
	OneAtATime: "One agent: this computer can't run more than one at a time with this model.",
	OnePart:    "One agent: this request doesn't split into parts that can be done separately.",
	OneModel:   "One agent: a race needs at least two models to compare.",
	TaskLater:  "One agent: swarms work on questions for now. A swarm for tasks in a folder comes in a later version.",
	NoChat:     "One agent: a swarm keeps its agents in chats, and this request isn't saved in one.",
}

// Reason is the one line, in English, that says why a swarm ran as one agent.
func Reason(code string) string { return reasons[code] }

// Single says why a planned and placed swarm should run as one agent instead,
// as a code for Reason, or "" when its agents can run at once.
func Single(mode string, agents []Agent, lanes []Lane) string {
	switch {
	case len(agents) < 2 && mode == ModeRace:
		return OneModel
	case len(agents) < 2:
		return OnePart
	case AtOnce(lanes) < 2:
		return OneAtATime
	}
	return ""
}
