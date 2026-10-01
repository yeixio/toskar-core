package app

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/yeixio/yggdrasil-core/internal/mimir"
	"github.com/yeixio/yggdrasil-core/internal/muninn"
	"github.com/yeixio/yggdrasil-core/internal/tools"
	"github.com/yeixio/yggdrasil-core/pkg/contracts"
)

// Limits keep an answer's source list readable.
const (
	maxTurnSources = 8
	maxSnippetRune = 240
)

// turnTrace records what one chat turn drew on and did, so the answer can
// show its sources and a plain-language summary of the work. It also notes
// when the turn has taken in untrusted content (§58).
type turnTrace struct {
	mu        sync.Mutex
	sources   []contracts.Citation
	steps     []contracts.ActivityStep
	untrusted bool
}

func (t *turnTrace) addSource(c contracts.Citation) {
	key := c.Kind + "|" + c.URL + "|" + c.Source + "|" + c.Title
	for _, s := range t.sources {
		if s.Kind+"|"+s.URL+"|"+s.Source+"|"+s.Title == key {
			return
		}
	}
	if len(t.sources) < maxTurnSources {
		t.sources = append(t.sources, c)
	}
}

func (t *turnTrace) addStep(kind, text string) {
	t.steps = append(t.steps, contracts.ActivityStep{Kind: kind, Text: text})
}

// knowledge records passages retrieved from Mimir.
func (t *turnTrace) knowledge(hits []mimir.Hit) {
	if len(hits) == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.untrusted = true
	names := []string{}
	seen := map[string]bool{}
	for _, h := range hits {
		t.addSource(contracts.Citation{Kind: "knowledge", Title: h.Title, Source: h.SourceName, Snippet: snippet(h.Body)})
		if !seen[h.SourceName] {
			seen[h.SourceName] = true
			names = append(names, h.SourceName)
		}
	}
	t.addStep("knowledge", fmt.Sprintf("Found %s in %s", plural(len(hits), "passage", "passages"), joinNames(names)))
}

// memories records persistent memories given to the model.
func (t *turnTrace) memories(list []muninn.Memory) {
	if len(list) == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, m := range list {
		t.addSource(contracts.Citation{Kind: "memory", Title: m.Content, Source: "Memory"})
	}
	t.addStep("memory", "Used "+plural(len(list), "memory", "memories"))
}

// tool records a tool call that succeeded.
func (t *turnTrace) tool(toolID string, args, result map[string]any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	str := func(m map[string]any, k string) string {
		v, _ := m[k].(string)
		return strings.TrimSpace(v)
	}
	switch toolID {
	case "internet.search":
		t.untrusted = true
		t.addStep("search", fmt.Sprintf("Searched the web for “%s”", str(args, "query")))
		var rows []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Snippet string `json:"snippet"`
		}
		if raw, err := json.Marshal(result["results"]); err == nil {
			_ = json.Unmarshal(raw, &rows)
		}
		for i, r := range rows {
			if i == 3 || r.URL == "" {
				break
			}
			t.addSource(contracts.Citation{Kind: "web", Title: firstNonEmpty(r.Title, hostOf(r.URL)), URL: r.URL, Snippet: snippet(r.Snippet)})
		}
	case "internet.open":
		t.untrusted = true
		u, title := str(result, "url"), str(result, "title")
		if u == "" {
			u = str(args, "url")
		}
		title = firstNonEmpty(title, hostOf(u))
		t.addStep("read", fmt.Sprintf("Read “%s”", title))
		// A page that was read outranks the same page as a search hit.
		for i := range t.sources {
			if t.sources[i].Kind == "web" && t.sources[i].URL == u {
				t.sources[i].Title = title
				return
			}
		}
		t.addSource(contracts.Citation{Kind: "web", Title: title, URL: u, Snippet: snippet(str(result, "content"))})
	case "filesystem.read":
		t.untrusted = true
		p := str(args, "path")
		t.addStep("file", fmt.Sprintf("Read %s", p))
		t.addSource(contracts.Citation{Kind: "file", Title: p, Source: p})
	case "filesystem.search":
		t.addStep("file", fmt.Sprintf("Looked for files matching “%s”", str(args, "query")))
	case "filesystem.write":
		t.addStep("write", fmt.Sprintf("Saved %s", str(args, "path")))
	case "terminal":
		t.untrusted = true
		t.addStep("command", "Ran a command on this computer")
	default:
		if strings.HasPrefix(toolID, "git.") {
			t.untrusted = true
			t.addStep("git", "Checked the Git repository ("+strings.TrimPrefix(toolID, "git.")+")")
		}
	}
}

// effectivePolicy applies §58: after a turn has read untrusted content, a
// tool that changes something asks first even when the profile allows it.
// Read-only tools and Deny are unchanged.
func effectivePolicy(policy, toolID string, untrusted bool) string {
	if policy != tools.PolicyAllow || !untrusted {
		return policy
	}
	if def, ok := tools.Lookup(toolID); !ok || def.Risk != "read" {
		return tools.PolicyAsk
	}
	return policy
}

func (t *turnTrace) sawUntrusted() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.untrusted
}

// meta returns what to store with the answer, or nil when nothing was used.
func (t *turnTrace) meta() *contracts.MessageMeta {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.sources) == 0 && len(t.steps) == 0 {
		return nil
	}
	return &contracts.MessageMeta{
		Sources: append([]contracts.Citation(nil), t.sources...),
		Steps:   append([]contracts.ActivityStep(nil), t.steps...),
	}
}

func snippet(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= maxSnippetRune {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:maxSnippetRune])) + "…"
}

func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return strings.TrimPrefix(u.Host, "www.")
	}
	return raw
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

func joinNames(names []string) string {
	switch len(names) {
	case 0:
		return "your knowledge"
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	default:
		return fmt.Sprintf("%s and %d more", names[0], len(names)-1)
	}
}
