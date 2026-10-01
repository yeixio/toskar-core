package app

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/yeixio/yggdrasil-core/internal/artifacts"
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
	files     []contracts.FileRef
	notice    string
	untrusted bool
	// runID links the answer to its run trace (§35).
	runID string
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

// attachment records a file the user attached that the model read.
func (t *turnTrace) attachment(a artifacts.Artifact, picked, total int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.untrusted = true
	text := "Read " + a.Name
	if picked < total {
		text = fmt.Sprintf("Read the %d parts of %s that match the question", picked, a.Name)
	}
	t.addStep("file", text)
	source := sourceAttached
	if a.Producer == artifacts.ProducerAssistant {
		source = sourceMade
	}
	t.addSource(contracts.Citation{Kind: "file", Title: a.Name, Source: source})
}

// Source labels for files in a chat.
const (
	sourceAttached = "Attached file"
	sourceMade     = "Made in this chat"
)

// dataKind reports whether the answer drew on the user's own data: "file"
// for attached files, "knowledge" for connected knowledge, or "".
func (t *turnTrace) dataKind() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	kind := ""
	for _, s := range t.sources {
		switch {
		case s.Kind == "file" && (s.Source == sourceAttached || s.Source == sourceMade):
			return "file"
		case s.Kind == "knowledge":
			kind = "knowledge"
		}
	}
	return kind
}

// noticeIfNone sets the answer's notice unless one is already there, such
// as a note that a fallback model answered.
func (t *turnTrace) noticeIfNone(notice string) {
	if notice == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.notice == "" {
		t.notice = notice
	}
}

// planned records that a request was worked through in parts.
func (t *turnTrace) planned(parts int, parallel bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	text := fmt.Sprintf("Worked through the request in %d parts", parts)
	if parallel {
		text = fmt.Sprintf("Split the request into %d parts and looked them up side by side", parts)
	}
	t.addStep("plan", text)
}

// verified records an answer check (spec §24). Figures that could not be
// confirmed become the answer's notice.
func (t *turnTrace) verified(issues, fixed int, remaining string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch {
	case issues == 0:
		t.addStep("verify", "Checked the figures against the sources")
	case fixed > 0 && remaining == "":
		t.addStep("verify", "Checked the figures and corrected "+plural(fixed, "figure", "figures"))
	default:
		t.addStep("verify", "Checked the figures; some could not be confirmed")
	}
	if remaining != "" && t.notice == "" {
		t.notice = fmt.Sprintf("Yggdrasil could not confirm %s in the sources. Check before relying on it.", remaining)
	}
}

// stopped records that the user stopped the turn. kept says whether part
// of the answer was written and saved.
func (t *turnTrace) stopped(kept bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.addStep("stop", "Stopped by you")
	if kept {
		t.notice = "Stopped before the answer was finished."
	}
}

// effort records an effort the user chose. Auto's own choice is not listed.
func (t *turnTrace) effort(label string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.addStep("effort", "Worked at "+label+" effort, as you chose")
}

// sharing records that other work, such as training, is using this computer.
func (t *turnTrace) sharing(text string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.addStep("share", text)
}

// routed records which model Auto chose and why.
func (t *turnTrace) routed(reason string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.addStep("route", reason)
}

// recovered records that another model answered after one failed. notice is
// shown with the answer when the change may affect it.
func (t *turnTrace) recovered(step, notice string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.addStep("recover", step)
	if notice != "" {
		t.notice = notice
	}
}

// hasSideEffects reports whether the turn changed something, such as a file
// or a commit, so it must not be run again.
func (t *turnTrace) hasSideEffects() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, s := range t.steps {
		switch s.Kind {
		case "write", "create", "command", "git", "stop":
			return true
		}
	}
	return false
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
		if strings.EqualFold(title, "Untitled page") {
			title = ""
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
	case "files.create":
		name := str(result, "name")
		t.addStep("create", fmt.Sprintf("Created %s", name))
		size, _ := result["size_bytes"].(int64)
		t.files = append(t.files, contracts.FileRef{
			ID: str(result, "id"), Name: name, MimeType: str(result, "mime_type"), Kind: str(result, "kind"),
			Size: size, Producer: "assistant",
		})
	case "terminal":
		t.untrusted = true
		t.addStep("command", "Ran a command on this computer")
	default:
		if strings.HasPrefix(toolID, "git.") {
			t.untrusted = true
			t.addStep("git", "Checked the Git repository ("+strings.TrimPrefix(toolID, "git.")+")")
			return
		}
		if def, ok := tools.Lookup(toolID); ok && strings.HasPrefix(def.Source, "connector:") {
			// What a connected service returns was written by other people,
			// so it is data, not instructions (§58).
			t.untrusted = true
			t.addStep("service", def.Name)
			t.serviceSources(result)
		}
	}
}

// serviceSources cites the pages a connected service's result links to,
// such as GitHub issues.
func (t *turnTrace) serviceSources(result map[string]any) {
	add := func(m map[string]any) {
		u, _ := m["url"].(string)
		if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
			return
		}
		title, _ := m["title"].(string)
		t.addSource(contracts.Citation{Kind: "web", Title: firstNonEmpty(title, hostOf(u)), URL: u})
	}
	add(result)
	if list, ok := result["results"].([]any); ok {
		for i, item := range list {
			if m, ok := item.(map[string]any); ok && i < 5 {
				add(m)
			}
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
	if def, ok := tools.Lookup(toolID); !ok || !tools.Contained(def.Risk) {
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
	if len(t.sources) == 0 && len(t.steps) == 0 && t.notice == "" && len(t.files) == 0 && t.runID == "" {
		return nil
	}
	return &contracts.MessageMeta{
		Sources: append([]contracts.Citation(nil), t.sources...),
		Steps:   append([]contracts.ActivityStep(nil), t.steps...),
		Notice:  t.notice,
		Files:   append([]contracts.FileRef(nil), t.files...),
		RunID:   t.runID,
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
