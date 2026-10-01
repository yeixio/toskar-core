package simple

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/yeixio/yggdrasil-core/internal/tools"
	"github.com/yeixio/yggdrasil-core/pkg/contracts"
	"github.com/yeixio/yggdrasil-core/pkg/pluginapi"
)

// Limits for what a lookup hands the model.
const (
	lookupResults   = 5
	lookupPageRunes = 6000
	lookupQueryMax  = 200
)

// EventLookup tells the UI that Yggdrasil is looking something up before the
// model answers.
const EventLookup = "chat.lookup"

var askPrefixRe = regexp.MustCompile(`(?i)^\s*(hey|hi|ok|okay|so|please|can you|could you|would you|will you|search (the web|online) for|look up|find out about|find out|tell me about|tell me|i want to know|i'd like to know)[\s,:]+`)

// lookupQuery turns a question into a search query: polite openers removed,
// length capped.
func lookupQuery(prompt string) string {
	q := strings.TrimSpace(prompt)
	for i := 0; i < 4; i++ {
		next := askPrefixRe.ReplaceAllString(q, "")
		if next == q {
			break
		}
		q = next
	}
	q = strings.TrimRight(strings.TrimSpace(q), "?!. ")
	if utf8.RuneCountInString(q) > lookupQueryMax {
		q = string([]rune(q)[:lookupQueryMax])
	}
	if q == "" {
		return strings.TrimSpace(prompt)
	}
	return q
}

// lookUpFirst searches the web, and reads the best page, before the model
// answers a question that needs current information (spec §21). Small models
// often pick the wrong tool or none; Yggdrasil decides instead. It runs only
// when the profile allows web search without asking, and returns the material
// to give the model as data.
func lookUpFirst(ctx context.Context, env pluginapi.ExecutionEnvironment, profile contracts.AIProfile, prompt string) (string, bool) {
	if !tools.MessageNeedsLiveWeb(prompt) {
		return "", false
	}
	return lookUp(ctx, env, profile, lookupQuery(prompt), prompt)
}

// webAllowed reports whether the profile lets Yggdrasil search without asking.
func webAllowed(profile contracts.AIProfile) bool {
	return strings.EqualFold(tools.PolicyForProfile(profile, "internet.search"), tools.PolicyAllow)
}

// lookUp searches the web for query and reads the best page. prompt is the
// question the material is for.
func lookUp(ctx context.Context, env pluginapi.ExecutionEnvironment, profile contracts.AIProfile, query, prompt string) (string, bool) {
	if !webAllowed(profile) {
		return "", false
	}
	env.Emit(EventLookup, map[string]any{"query": query})
	args := map[string]any{"query": query}
	result, err := env.ExecuteTool(ctx, "internet.search", args)
	if err != nil {
		return "", false
	}
	var rows []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Snippet string `json:"snippet"`
	}
	if raw, err := json.Marshal(result["results"]); err == nil {
		_ = json.Unmarshal(raw, &rows)
	}
	if len(rows) == 0 {
		return "", false
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Web search results for “%s”:\n", query)
	for i, r := range rows {
		if i == lookupResults {
			break
		}
		fmt.Fprintf(&b, "- %s (%s): %s\n", strings.TrimSpace(r.Title), r.URL, strings.TrimSpace(r.Snippet))
	}
	if page, ok := readBestPage(ctx, env, profile, prompt, args, result); ok {
		title, _ := page["title"].(string)
		link, _ := page["url"].(string)
		content, _ := page["content"].(string)
		if utf8.RuneCountInString(content) > lookupPageRunes {
			content = string([]rune(content)[:lookupPageRunes]) + "…"
		}
		fmt.Fprintf(&b, "\nPage read: %s (%s)\n%s\n", strings.TrimSpace(title), link, strings.TrimSpace(content))
	}
	return b.String(), true
}

// lookupGuidance tells the model how to use what was looked up.
const lookupGuidance = "Yggdrasil already searched the web for this question; the results are in the reference material. Answer from them directly: state the facts and include the most relevant link. Do not mention the search, the results, or reference material. If they do not answer the question, say so."

// withoutWeb removes web tools for a turn whose lookup is already done, so a
// small model answers instead of searching again.
func withoutWeb(profile contracts.AIProfile) contracts.AIProfile {
	out := profile
	out.Tools = make([]contracts.ToolPolicy, 0, len(profile.Tools))
	for _, t := range profile.Tools {
		if t.ToolID == "internet.search" || t.ToolID == "internet.open" {
			continue
		}
		out.Tools = append(out.Tools, t)
	}
	return out
}

func joinReference(parts ...string) string {
	var keep []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			keep = append(keep, strings.TrimSpace(p))
		}
	}
	return strings.Join(keep, "\n\n")
}
