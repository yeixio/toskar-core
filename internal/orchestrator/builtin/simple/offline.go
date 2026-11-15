package simple

import (
	"context"
	"strings"

	"github.com/yeixio/toskar-core/internal/offline"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// lookups is what a turn's look-ups on the internet came to (#965): the
// ones that failed because this computer isn't online, and whether any
// found something. Offline, a small model told the news couldn't be read
// wrote the day's AI news anyway, made up.
type lookups struct {
	failed  []string
	fetched bool
}

type lookupsKey struct{}

func withLookups(ctx context.Context) context.Context {
	return context.WithValue(ctx, lookupsKey{}, &lookups{})
}

func lookupsFrom(ctx context.Context) *lookups {
	if l, ok := ctx.Value(lookupsKey{}).(*lookups); ok {
		return l
	}
	return &lookups{}
}

// cutOff reports a turn whose look-ups all failed for want of the
// internet, so there's nothing to answer from but memory.
func (l *lookups) cutOff() bool {
	return len(l.failed) > 0 && !l.fetched
}

// runTool runs a tool and notes how a look-up went. Opening a page counts
// for neither: a mistyped address fails its lookup as offline does.
func runTool(ctx context.Context, env pluginapi.ExecutionEnvironment, id string, args map[string]any) (map[string]any, error) {
	result, err := env.ExecuteTool(ctx, id, args)
	if def, ok := tools.Lookup(id); ok && def.Capability == tools.CapInternet && id != "internet.open" {
		l := lookupsFrom(ctx)
		switch {
		case err == nil:
			l.fetched = true
		case offline.Is(err):
			l.failed = append(l.failed, id)
		}
	}
	return result, err
}

// offlineTell asks for the answer when look-ups couldn't reach the
// internet: say so, and don't answer from memory as if they had.
func offlineTell(failed []string) string {
	what := "it"
	for _, id := range failed {
		if id == "news.read" {
			what = "the AI news"
			break
		}
		if def, ok := tools.Lookup(id); ok && def.Capability == tools.CapInternet {
			what = "it on the internet"
		}
	}
	return "This computer isn't connected to the internet right now, so Toskar couldn't look " + what + " up." +
		"\nIn one or two short sentences, in the language of the user's last message, tell them that, and that they can ask again once it's back online." +
		" Don't answer the question from memory, and don't make up news, prices, or other current facts."
}

const offlineFallback = "I couldn't look that up: this computer isn't connected to the internet right now. Ask again once it's back online."

// answerOffline writes the answer for a turn cut off from the internet.
func answerOffline(ctx context.Context, env pluginapi.ExecutionEnvironment, role, prompt string) (string, *pluginapi.GenerationMetrics) {
	ask := []pluginapi.ChatMessage{{Role: "user", Content: prompt}}
	// A sentence or two needs no thinking: a 4B model thought for 8,000
	// tokens, over two minutes, before saying it was offline.
	o := pluginapi.GenerateOptionsFrom(ctx)
	o.NoThinking, o.Thinking, o.ReasoningEffort = true, false, ""
	o.MaxTokens = 200
	reply, m, err := generateText(pluginapi.WithGenerateOptions(ctx, o), env, role, withInstruction(ask, offlineTell(lookupsFrom(ctx).failed)))
	if err != nil || strings.TrimSpace(reply) == "" {
		return offlineFallback, m
	}
	return strings.TrimSpace(reply), m
}
