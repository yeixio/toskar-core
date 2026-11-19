package huginn

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/yeixio/toskar-core/internal/tools"
)

// Plan is how Huginn splits a request that has several parts (spec §22–23).
type Plan struct {
	// Steps are the parts, in order.
	Steps []string
	// Parallel is true when the steps are independent, such as researching
	// several products; otherwise each step builds on the ones before it.
	Parallel bool
	// NeedsWeb is true when the steps should look things up.
	NeedsWeb bool
}

// maxSteps caps a plan so a long list cannot run away.
const maxSteps = 5

var (
	// compareRe finds "compare A, B and C [for X]" and similar requests about
	// several subjects at once.
	compareRe   = regexp.MustCompile(`(?i)^(?:please\s+|can you\s+|could you\s+|help me\s+)?(compare|comparing|research|look into|look up|evaluate|review|summarize|find out about|tell me about)\s+(.+?)(?:\s+(?:for|on|by|in terms of|regarding|when it comes to|with respect to)\s+(.+?))?[.?!]*$`)
	itemSplitRe = regexp.MustCompile(`(?i)\s*(?:,\s*(?:and|or)?\s*|\s+and\s+|\s+or\s+|\s+vs\.?\s+|\s+versus\s+)\s*`)
	// stepSplitRe splits "do A, then B, then C" and numbered lists.
	stepSplitRe = regexp.MustCompile(`(?i)(?:[,;]?\s*\b(?:and then|then|after that|afterwards|next|finally)\b[,:]?\s*|;\s*|\n\s*(?:\d+[.)]|[-*])\s+)`)
	webVerbRe   = regexp.MustCompile(`(?i)\b(find|research|compare|comparison|versus|vs|look up|search|prices?|reviews?|latest|current|specs?|benchmarks?)\b`)
)

// MakePlan returns a plan for a request with several parts, or false when it
// is better answered in one go. Simple requests stay simple.
// quickLength is the longest one-line question with steps that's still
// answered at once, without a plan.
const quickLength = 100

func MakePlan(message string) (Plan, bool) {
	msg := strings.TrimSpace(message)
	if utf8.RuneCountInString(msg) < 20 {
		return Plan{}, false
	}
	needsWeb := tools.MessageNeedsLiveWeb(msg) || webVerbRe.MatchString(msg)

	// A sequence of different steps: each builds on the last.
	var steps []string
	for _, part := range stepSplitRe.Split(msg, -1) {
		part = strings.Trim(strings.TrimSpace(part), ",.;:")
		if len(strings.Fields(part)) >= 2 {
			steps = append(steps, part)
		}
	}
	// Two quick questions on one line are answered together, not as a plan
	// of steps (#1346): "What is 17 times 23? Then say what model you are."
	// Work to do in order, such as an outline then a schedule, has no
	// question in it.
	quick := !needsWeb && strings.Contains(msg, "?") && !strings.Contains(msg, "\n") && utf8.RuneCountInString(msg) <= quickLength
	if len(steps) >= 2 && !quick {
		if len(steps) > maxSteps {
			steps = steps[:maxSteps]
		}
		return Plan{Steps: steps, NeedsWeb: needsWeb}, true
	}
	// Several subjects with one question each: independent, so in parallel.
	if m := compareRe.FindStringSubmatch(strings.SplitN(msg, "\n", 2)[0]); m != nil {
		items := subjects(m[2])
		if len(items) >= 2 {
			aspect := strings.TrimSpace(m[3])
			steps := make([]string, 0, len(items))
			for _, it := range items {
				step := "Find out about " + it
				if aspect != "" {
					step += " for " + aspect
				}
				steps = append(steps, step)
			}
			return Plan{Steps: steps, Parallel: true, NeedsWeb: needsWeb}, true
		}
	}

	return Plan{}, false
}

// subjects splits "Ollama, llama.cpp, MLX and vLLM" into its items. Items must
// be short, so a sentence with commas is not mistaken for a list.
func subjects(text string) []string {
	text = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), "between "))
	var out []string
	for _, it := range itemSplitRe.Split(text, -1) {
		it = strings.Trim(strings.TrimSpace(it), ".?!")
		it = strings.TrimPrefix(it, "the ")
		if it == "" {
			continue
		}
		if len(strings.Fields(it)) > 5 {
			return nil
		}
		out = append(out, it)
	}
	if len(out) > maxSteps {
		out = out[:maxSteps]
	}
	return out
}
