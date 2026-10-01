package simple

import (
	"context"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/yeixio/yggdrasil-core/internal/huginn"
	"github.com/yeixio/yggdrasil-core/internal/tools"
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
func verifyAnswer(ctx context.Context, env pluginapi.ExecutionEnvironment, role string, messages []pluginapi.ChatMessage, answer, evidence, prompt string) string {
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
	env.Emit(EventVerifying, map[string]any{"issues": len(issues)})
	ask := append(append([]pluginapi.ChatMessage(nil), messages...),
		pluginapi.ChatMessage{Role: "assistant", Content: answer},
		pluginapi.ChatMessage{Role: "user", Content: "Check your answer against the reference material. Problems:\n" + huginn.Describe(issues) +
			"Rewrite the whole answer for the user. Use only figures from the reference material, or calculations done correctly from them. " +
			"If a figure is not in the reference material, say it is not there instead of guessing. Reply with the answer only."},
	)
	revised, _, err := generateText(ctx, env, role, ask)
	remaining := issues
	if err == nil {
		// A revision must still be an answer: one that fixes figures by
		// dropping most of the reply is not kept.
		if text := tools.ParseModelOutput(revised).Text; substantial(text, answer) {
			if again := huginn.Check(text, evidence, prompt); len(again) < len(issues) {
				answer, remaining = text, again
			}
		}
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

// toolNameRe matches tool ids written out in an answer's text.
var toolNameRe = regexp.MustCompile(`\b(internet\.(search|open)|filesystem\.(read|write|search)|git\.(status|diff|log|show|add|commit|push)|files\.create|tool_call)\b`)

// narratesTools reports an answer that describes tool calls, such as
// "use internet.search to find the price", instead of answering.
func narratesTools(answer string) bool {
	return len(toolNameRe.FindAllString(answer, -1)) >= 1
}
