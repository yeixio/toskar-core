package huginn

import (
	"regexp"
	"strings"
)

// TaskToolCalls is what a task may spend. It's there to stop a runaway, not to
// end ordinary work, which ends when the model says it's done or stuck.
const TaskToolCalls = 100

// StuckAfter is how many times the same call may give the same result.
const StuckAfter = 3

// taskRe asks for work to be done, by its first word.
var taskRe = regexp.MustCompile(`(?i)^\W*(please\s+)?(can you\s+|could you\s+)?(fix|debug|set up|setup|install|build|update|upgrade|refactor|add|implement|migrate|port|rename|clean( up)?|clear|find and|free up|run|execute|get (it|this|the \w+) (working|running|passing)|configure|deploy|test)\b`)

// IsTask reports a message from the terminal that asks for work to be done on
// this computer, step by step (#1017). A question stays a question, and small
// models offered tools for one reach for them.
func IsTask(k Kind, message, client string) bool {
	if client != "terminal" {
		return false
	}
	return k == Local || k == Coding || taskRe.MatchString(strings.TrimSpace(message))
}

// TaskBudget is the budget for a task: one long loop of doing and checking, so
// no plan, and no check of its figures, which is for answers rather than work.
// A cap the profile sets still holds.
func (b Budget) TaskBudget(profileCap int) Budget {
	b.Plan, b.Verify, b.Corrections = false, false, 0
	if profileCap <= 0 {
		b.MaxToolCalls = TaskToolCalls
	}
	return b
}

// TaskTools are what a task can use throughout, whatever the first message
// happened to say: it can turn out to need any of them, and each still asks as
// its policy says.
func TaskTools(k Kind, message string, available []string) []string {
	offered := ToolsFor(k, message, available)
	for _, g := range []string{"read", "write", "create", "shell", "gitr", "gitw", "web"} {
		for _, id := range toolGroups[g] {
			for _, a := range available {
				if a == id && !contains(offered, id) {
					offered = append(offered, id)
				}
			}
		}
	}
	return offered
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
