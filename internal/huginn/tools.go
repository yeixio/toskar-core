package huginn

import (
	"regexp"
	"slices"
	"strings"
)

// Tool groups, by capability. A request is offered whole groups.
var toolGroups = map[string][]string{
	"web":    {"internet.search", "internet.open"},
	"read":   {"filesystem.search", "filesystem.read"},
	"write":  {"filesystem.write"},
	"create": {"files.create"},
	"shell":  {"terminal"},
	"gitr":   {"git.status", "git.diff", "git.log", "git.show"},
	"gitw":   {"git.add", "git.commit", "git.push"},
}

// Groups each kind of request gets before cues in the message add more.
var kindGroups = map[Kind][]string{
	Chat:     {"web", "create"},
	Current:  {"web", "create"},
	Research: {"web", "create", "read"},
	Coding:   {"web", "create", "read", "write", "shell", "gitr"},
	Local:    {"read", "write", "create", "shell", "gitr", "gitw"},
}

var (
	cueRead  = regexp.MustCompile(`(?i)(\b(files?|folders?|director(y|ies)|documents?|workspace|repo|repository|project|readme|log file)\b|[~./][\w./-]*/[\w.-]+|\b\w+\.(go|py|js|ts|tsx|md|txt|json|ya?ml|toml|csv|log|sh)\b)`)
	cueWrite = regexp.MustCompile(`(?i)\b(save|write|edit|update|change|fix|rename|append|create)\b.{0,40}\b(file|files|folder|config|readme|script)\b`)
	cueShell = regexp.MustCompile(`(?i)\b(run|execute|install|build|compile|terminal|command|shell|script|npm|pnpm|pip|brew|make|go test|go build)\b`)
	cueGit   = regexp.MustCompile(`(?i)\b(git|commit|branch|diff|merge|rebase|staged|push|pull request)\b`)
	cueGitW  = regexp.MustCompile(`(?i)\b(commit|stage|push)\b`)
	cueWeb   = regexp.MustCompile(`(?i)(\b(search|web|online|internet|look up|website|url|link|news|latest)\b|https?://)`)
)

// ToolsFor picks the tools worth offering for a request (spec §16): the
// groups its kind needs, plus any the message asks for, limited to the
// tools the profile has. Offering fewer tools keeps small models from
// reaching for the wrong one, and keeps the prompt short.
func ToolsFor(k Kind, message string, available []string) []string {
	want := map[string]bool{}
	for _, g := range kindGroups[k] {
		want[g] = true
	}
	if cueRead.MatchString(message) {
		want["read"] = true
	}
	if cueWrite.MatchString(message) {
		want["read"], want["write"] = true, true
	}
	if cueShell.MatchString(message) {
		want["shell"] = true
	}
	if cueGit.MatchString(message) {
		want["gitr"] = true
		if cueGitW.MatchString(message) {
			want["gitw"] = true
		}
	}
	if cueWeb.MatchString(message) {
		want["web"] = true
	}
	var out []string
	for _, id := range available {
		for g, ids := range toolGroups {
			if want[g] && slices.Contains(ids, id) {
				out = append(out, id)
				break
			}
		}
		// Tools outside the built-in groups, such as connected services,
		// are offered when the message names them.
		if !grouped(id) && strings.Contains(strings.ToLower(message), strings.SplitN(id, ".", 2)[0]) {
			out = append(out, id)
		}
	}
	return out
}

func grouped(id string) bool {
	for _, ids := range toolGroups {
		if slices.Contains(ids, id) {
			return true
		}
	}
	return false
}
