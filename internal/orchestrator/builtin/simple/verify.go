package simple

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/yeixio/yggdrasil-core/internal/huginn"
	"github.com/yeixio/yggdrasil-core/internal/tools"
	"github.com/yeixio/yggdrasil-core/pkg/contracts"
	"github.com/yeixio/yggdrasil-core/pkg/pluginapi"
)

// Events for answer checks (spec §24).
const (
	EventVerifying = "chat.verifying"
	EventVerified  = "verify.done"
)

// verifyAnswer checks an answer's arithmetic and, when the turn used
// reference material or tool results, that its figures appear in them. The
// check needs no model; only an answer with issues is sent back to the model
// once, with the issues named, and the revision is kept if it has fewer.
// Figures that still do not check out are reported for the answer's note.
func verifyAnswer(ctx context.Context, env pluginapi.ExecutionEnvironment, role string, messages []pluginapi.ChatMessage, answer, evidence, prompt string, corrections int) string {
	if !huginn.HasFigures(answer) {
		return answer
	}
	issues := huginn.Check(answer, evidence, prompt)
	if len(issues) == 0 {
		if evidence != "" {
			env.Emit(EventVerified, map[string]any{"issues": 0})
		}
		return answer
	}
	remaining := issues
	// Each pass sends the issues back once; a revision is kept only when it
	// fixes some and is still a real answer (§24). Fast skips the passes and
	// only reports what did not check out.
	for pass := 0; pass < corrections && len(remaining) > 0 && ctx.Err() == nil; pass++ {
		env.Emit(EventVerifying, map[string]any{"issues": len(remaining)})
		ask := append(append([]pluginapi.ChatMessage(nil), messages...),
			pluginapi.ChatMessage{Role: "assistant", Content: answer},
			pluginapi.ChatMessage{Role: "user", Content: "Check your answer against the reference material. Problems:\n" + huginn.Describe(remaining) +
				"Rewrite the whole answer for the user. Use only figures from the reference material, or calculations done correctly from them. " +
				"If a figure is not in the reference material, say it is not there instead of guessing. Reply with the answer only."},
		)
		revised, _, err := generateText(ctx, env, role, ask)
		if err != nil {
			break
		}
		// A revision must still be an answer: one that fixes figures by
		// dropping most of the reply is not kept.
		text := tools.ParseModelOutput(revised).Text
		if !substantial(text, answer) {
			break
		}
		again := huginn.Check(text, evidence, prompt)
		if len(again) >= len(remaining) {
			break
		}
		answer, remaining = text, again
	}
	env.Emit(EventVerified, map[string]any{
		"issues":    len(issues),
		"fixed":     len(issues) - len(remaining),
		"remaining": huginn.Figures(remaining),
	})
	return answer
}

func substantial(revised, original string) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(revised))
	return n >= 20 && n*10 >= utf8.RuneCountInString(original)*4
}

// toolNameRe matches tool ids, and their capability aliases, written out in
// an answer's text.
var toolNameRe = regexp.MustCompile(`\b(internet\.(search|open)|filesystem\.(read|write|search)|git\.(status|diff|log|show|add|commit|push)|files?\.(create|read|write|search)|web\.(search|open|fetch)|shell\.(run|execute)|tool_call)\b`)

// looseCallRe finds a call written as prose, such as files.search {"query":"x"}.
var looseCallRe = regexp.MustCompile(`([a-z]+\.[a-z_]+)\s*(\{[^{}]*\})`)

// looseToolCall reads a short answer that is only a tool call written as
// prose, with the id and its JSON arguments, as the call it meant. It is
// accepted only for an offered tool, so it cannot reach anything else.
func looseToolCall(content string, profile contracts.AIProfile) (*tools.ModelCall, bool) {
	if utf8.RuneCountInString(content) > 600 {
		return nil, false
	}
	m := looseCallRe.FindStringSubmatch(content)
	if m == nil {
		return nil, false
	}
	id := tools.Canonical(m[1])
	if !toolEnabled(profile, id) {
		return nil, false
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(m[2]), &args); err != nil {
		return nil, false
	}
	return &tools.ModelCall{ID: id, Args: args}, true
}

// narratesTools reports an answer that describes tool calls, such as
// "use internet.search to find the price", instead of answering.
func narratesTools(answer string) bool {
	if toolNameRe.MatchString(answer) {
		return true
	}
	// Connected services' tools, such as homeassistant.states.
	for _, def := range tools.ConnectedDefinitions() {
		if strings.Contains(answer, def.ID) {
			return true
		}
	}
	return false
}
