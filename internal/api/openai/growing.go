package openai

import (
	"strings"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// growingText turns a live answer, which may take back what it showed,
// into text that only grows, for clients that can't take text back
// (#1450). A new attempt that repeats what was sent goes on from where it
// left off; one that says something else follows it after a blank line.
type growingText struct {
	// prior is what had been sent when the current attempt began.
	prior string
	// attempt is the current attempt's text so far.
	attempt string
	// joined says the attempt is being sent: past prior, or after it.
	joined bool
	sent   strings.Builder
}

// next is the text to send for chunk.
func (g *growingText) next(chunk pluginapi.ChatChunk) string {
	if chunk.Replace {
		g.prior, g.attempt, g.joined = g.sent.String(), "", false
	}
	if chunk.Content == "" {
		return ""
	}
	g.attempt += chunk.Content
	out := chunk.Content
	if !g.joined {
		switch {
		case strings.HasPrefix(g.attempt, g.prior):
			out = g.attempt[len(g.prior):]
		case strings.HasPrefix(g.prior, g.attempt):
			// So far it says again what was sent.
			return ""
		default:
			out = "\n\n" + g.attempt
		}
		g.joined = true
	}
	g.sent.WriteString(out)
	return out
}
