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

// taskRe asks for work to be done, by the first word of the message or of a
// later sentence, as in "The test in sum_test.go fails. Fix Sum so it passes."
// (#1135).
var taskRe = regexp.MustCompile(`(?i)(^|[.!?;]\s|\n)\W*(please\s+)?(can you\s+|could you\s+)?(fix|debug|set up|setup|install|build|update|upgrade|refactor|add|implement|migrate|port|rename|clean( up)?|clear|find and|free up|run|execute|get (it|this|the \w+) (working|running|passing)|configure|deploy|test)\b`)

// goOnRe goes on with work asked for before, such as "go ahead and make that
// fix" after an answer that said what to change (#1111): it isn't a question.
var goOnRe = regexp.MustCompile(`(?i)^\W*((ok(ay)?|alright|great|thanks|good)\b[\s,.!]*)?(please\s+)?(yes|yep|yeah|sure|go ahead|go on|go for it|do it|do that|do so|proceed|continue|carry on|keep going|try again|apply|make (that|the|this|those|these|it)\b|change it|finish|let'?s do)\b`)

// codeWorkRe asks for code to be made or changed: a verb that starts a
// sentence, then a code file or something code is made of, such as "Write
// Reverse in strs/strs.go" or "Make the tests for price/price.go pass".
var codeWorkRe = regexp.MustCompile(`(?i)(^|[.!?;]\s|\n)\W*(please\s+)?((can|could|would) you\s+)?(make|write|change|edit|modify|rewrite|correct|repair|remove|delete|finish)\b[^.?!\n]{0,60}?(\b[\w-]+\.(go|py|js|jsx|ts|tsx|rs|rb|java|kt|swift|c|h|cc|cpp|cs|php|sh|sql)\b|\b(functions?|methods?|class(es)?|structs?|tests?|modules?|packages?|the build|(the|this|my|that) code)\b)`)

// failRe says that tests or a build fail, and fixAskRe asks for a fix, as a
// request rather than "how do I fix it?": together they ask for work.
var (
	failRe   = regexp.MustCompile(`(?i)\b(\w*tests?\w*|build|compil\w+|lint\w*|ci|checks?)\b[^.?!\n]{0,40}?\b(fail\w*|broken|breaks|panic\w*|(does ?n[o']t|won'?t|can'?t) (pass|compile|build|run))\b`)
	fixAskRe = regexp.MustCompile(`(?i)(^|[.!?;,:]\s*|\s[-–—]\s*|\b(and|then|so|please|can you|could you|would you|help me)\s+)(fix|repair|resolve|sort out)\b`)
)

// IsTask reports a message that asks for work to be done on this computer, step
// by step (#1017): from the terminal, or in a chat that has a folder to work in. A question stays a question, and small
// models offered tools for one reach for them.
func IsTask(k Kind, message, client, project string) bool {
	if !WorksOnComputer(client, project) {
		return false
	}
	msg := strings.TrimSpace(message)
	return k == Local || k == Coding || taskRe.MatchString(msg) || goOnRe.MatchString(msg) ||
		codeWorkRe.MatchString(msg) || (failRe.MatchString(msg) && fixAskRe.MatchString(msg))
}

// WorksOnComputer reports a turn that can work on this computer as a task can:
// from the terminal, or in a chat with a folder.
func WorksOnComputer(client, project string) bool {
	return client == "terminal" || project != ""
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
