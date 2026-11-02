package simple

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

const (
	// taskFollowUp keeps a task going after each step.
	taskFollowUp = "\n\nContinue the task: if there's more to do, make the next tool call, and if something failed, read why and try to fix it. " +
		"When it's finished, or you can't get further, stop calling tools and say in plain words what you did, what the result was, and what's left, if anything."
	// taskStopped ends one, saying why.
	taskStopped = "\n\nStop here, because %s. Don't call any more tools. Say in plain words what you did, what the result was, and what's left to do."
	// keptSteps is how many latest steps a task keeps whole, and maxResult
	// how much of one result: a long task has to fit the model's context.
	keptSteps = 8
	maxResult = 8000
)

// taskTracker notices a task going round in circles.
type taskTracker struct {
	// start is where the task's steps begin in the messages.
	start int
	seen  map[string]int
	// stopped is why the task was told to stop, and steps what ran.
	stopped string
	steps   []string
}

// ran notes a step for the report.
func (k *taskTracker) ran(id string, args map[string]any, err error) {
	what := id
	if c, _ := args["command"].(string); c != "" {
		what = c
	}
	if err != nil {
		what += " (failed)"
	}
	k.steps = append(k.steps, oneLine(what))
}

// report says what ran, for a model that won't: the last steps of a long task.
func (k *taskTracker) report() string {
	steps := k.steps[max(0, len(k.steps)-20):]
	return "I stopped because " + k.stopped + ". Check what's left before going on. The last steps:\n\n- " + strings.Join(steps, "\n- ")
}

// repeated counts a step, and reports one that has now given the same result
// too many times. Running the tests again after a change is fine; getting the
// same answer again and again isn't progress.
func (k *taskTracker) repeated(id string, args map[string]any, result string) bool {
	if k.seen == nil {
		k.seen = map[string]int{}
	}
	raw, _ := json.Marshal(args)
	key := fmt.Sprintf("%s %s %s", id, raw, result)
	k.seen[key]++
	return k.seen[key] >= huginn.StuckAfter
}

// shortenResult keeps a result's start and, mostly, its end, where a command's
// errors and summary are.
func shortenResult(note string) string {
	if len(note) <= maxResult {
		return note
	}
	head, tail := maxResult/4, maxResult*3/4
	for head > 0 && !utf8.RuneStart(note[head]) {
		head--
	}
	cut := len(note) - tail
	for cut < len(note) && !utf8.RuneStart(note[cut]) {
		cut++
	}
	return note[:head] + fmt.Sprintf("\n[%d bytes left out]\n", cut-head) + note[cut:]
}

// foldOlderSteps replaces all but the latest steps with one line each, so a
// long task still fits the model's context. Each step is an assistant message
// with its call and a user message with the result.
func foldOlderSteps(messages []pluginapi.ChatMessage, start int) []pluginapi.ChatMessage {
	steps := (len(messages) - start) / 2
	if steps <= keptSteps {
		return messages
	}
	cut := start + (steps-keptSteps)*2
	var earlier strings.Builder
	for i := start; i+1 < cut; i += 2 {
		if strings.HasPrefix(messages[i].Content, foldedPrefix) {
			earlier.WriteString(strings.TrimPrefix(messages[i].Content, foldedPrefix))
			continue
		}
		outcome := "ok"
		if strings.HasPrefix(messages[i+1].Content, "The tool failed") {
			outcome = "failed"
		}
		fmt.Fprintf(&earlier, "- %s: %s\n", oneLine(messages[i].Content), outcome)
	}
	out := make([]pluginapi.ChatMessage, 0, start+2+keptSteps*2)
	out = append(out, messages[:start]...)
	out = append(out, pluginapi.ChatMessage{Role: "assistant", Content: foldedPrefix + earlier.String()},
		pluginapi.ChatMessage{Role: "user", Content: "Go on with the task from the steps below."})
	return append(out, messages[cut:]...)
}

const foldedPrefix = "Earlier steps:\n"

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 200 {
		return string(r[:200]) + "…"
	}
	return s
}
