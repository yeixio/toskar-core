package tools

import (
	"strings"

	"github.com/yeixio/yggdrasil-core/pkg/contracts"
)

// Capability IDs are the user-facing groups. Individual tools stay the model API.
const (
	CapInternet = "internet"
	CapFiles    = "files"
	CapShell    = "shell"
	CapGit      = "git"
)

// Definition is one registered tool. Future MCP tools use the same shape.
type Definition struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Capability    string `json:"capability"`
	Source        string `json:"source"`
	Schema        string `json:"schema"`
	DefaultPolicy string `json:"default_policy"`
	Risk          string `json:"risk"` // read | create | write
}

// Risk levels. A create tool only adds a file to Yggdrasil's own store, so it
// changes nothing else on the computer.
const (
	RiskRead   = "read"
	RiskCreate = "create"
	RiskWrite  = "write"
)

// Contained reports whether a tool cannot change anything outside
// Yggdrasil: it reads, or it only creates a file in Yggdrasil's store.
func Contained(risk string) bool { return risk == RiskRead || risk == RiskCreate }

// BuiltinCatalog is the single list of tools shipped with Yggdrasil.
func BuiltinCatalog() []Definition {
	return []Definition{
		{ID: "internet.search", Name: "Web Search", Description: "Search the public internet and return titles, links, and snippets.", Capability: CapInternet, Source: "builtin", Schema: `{"query":"string"}`, DefaultPolicy: PolicyAllow, Risk: "read"},
		{ID: "internet.open", Name: "Open Web Page", Description: "Open a web page and return readable text.", Capability: CapInternet, Source: "builtin", Schema: `{"url":"string"}`, DefaultPolicy: PolicyAllow, Risk: "read"},
		{ID: "filesystem.search", Name: "Find Files", Description: "Search file names in the workspace.", Capability: CapFiles, Source: "builtin", Schema: `{"query":"string"}`, DefaultPolicy: PolicyAllow, Risk: "read"},
		{ID: "filesystem.read", Name: "Read File", Description: "Read a file in the workspace.", Capability: CapFiles, Source: "builtin", Schema: `{"path":"string"}`, DefaultPolicy: PolicyAllow, Risk: "read"},
		{ID: "filesystem.write", Name: "Write File", Description: "Create or replace a file in the workspace.", Capability: CapFiles, Source: "builtin", Schema: `{"path":"string","content":"string"}`, DefaultPolicy: PolicyAllow, Risk: "write"},
		{ID: "files.create", Name: "Create File", Description: "Create a file the user can download: a document (.md, .txt, .html), data (.json, .csv), a spreadsheet (.xlsx, given as CSV text), or code. Use it when the user asks for a file, a spreadsheet, or a document.", Capability: CapFiles, Source: "builtin", Schema: `{"name":"string","content":"string"}`, DefaultPolicy: PolicyAllow, Risk: RiskCreate},
		{ID: "terminal", Name: "Terminal", Description: "Run a shell command on this computer.", Capability: CapShell, Source: "builtin", Schema: `{"command":"string"}`, DefaultPolicy: PolicyAllow, Risk: "write"},
		{ID: "git.status", Name: "Git Status", Description: "Show changed files in the workspace.", Capability: CapGit, Source: "builtin", Schema: `{}`, DefaultPolicy: PolicyAllow, Risk: "read"},
		{ID: "git.diff", Name: "Git Diff", Description: "Show the current git diff.", Capability: CapGit, Source: "builtin", Schema: `{"path":"string"}`, DefaultPolicy: PolicyAllow, Risk: "read"},
		{ID: "git.log", Name: "Git Log", Description: "Show recent commits.", Capability: CapGit, Source: "builtin", Schema: `{"path":"string"}`, DefaultPolicy: PolicyAllow, Risk: "read"},
		{ID: "git.show", Name: "Git Show", Description: "Show one commit.", Capability: CapGit, Source: "builtin", Schema: `{"revision":"string"}`, DefaultPolicy: PolicyAllow, Risk: "read"},
		{ID: "git.add", Name: "Git Add", Description: "Stage files for commit.", Capability: CapGit, Source: "builtin", Schema: `{"path":"string"}`, DefaultPolicy: PolicyAllow, Risk: "write"},
		{ID: "git.commit", Name: "Git Commit", Description: "Create a git commit.", Capability: CapGit, Source: "builtin", Schema: `{"message":"string"}`, DefaultPolicy: PolicyAllow, Risk: "write"},
		{ID: "git.push", Name: "Git Push", Description: "Push commits to the remote.", Capability: CapGit, Source: "builtin", Schema: `{"remote":"string","branch":"string"}`, DefaultPolicy: PolicyAllow, Risk: "write"},
	}
}

func Lookup(id string) (Definition, bool) {
	for _, def := range BuiltinCatalog() {
		if def.ID == id {
			return def, true
		}
	}
	return Definition{}, false
}

// PolicyForProfile returns the stored policy, or deny when the profile does not list the tool.
func PolicyForProfile(profile contracts.AIProfile, toolID string) string {
	for _, tool := range profile.Tools {
		if tool.ToolID == toolID {
			if strings.TrimSpace(tool.Policy) == "" {
				return PolicyDeny
			}
			return tool.Policy
		}
	}
	return PolicyDeny
}

// Enabled reports tools this profile will actually expose to a model.
func Enabled(profile contracts.AIProfile, globallyDisabled map[string]struct{}) []Definition {
	var out []Definition
	for _, def := range BuiltinCatalog() {
		if _, off := globallyDisabled[def.ID]; off {
			continue
		}
		if strings.EqualFold(PolicyForProfile(profile, def.ID), PolicyDeny) {
			continue
		}
		out = append(out, def)
	}
	return out
}

// PromptFor tells the model only about tools this profile actually has.
func PromptFor(profile contracts.AIProfile) string {
	enabled := Enabled(profile, nil)
	if len(enabled) == 0 {
		return ""
	}
	var lines []string
	internet := false
	for _, def := range enabled {
		if def.Capability == CapInternet {
			internet = true
		}
		lines = append(lines, "- "+def.ID+" "+def.Schema+" — "+def.Description)
	}
	prompt := "You have access to tools. Use them when they improve the accuracy or freshness of your answer.\n" +
		"When a tool is needed, call it directly. Do not explain that you are about to use a tool unless doing so is important to the user. Never expose function-call syntax, tool names, internal JSON, or execution protocol in your response.\n" +
		"To call a tool, reply with ONLY this JSON and nothing else:\n" +
		`{"tool_call":{"id":"<tool_id>","args":{...}}}` + "\n" +
		"Available tools:\n" + strings.Join(lines, "\n") + "\n" +
		"After tool results arrive, answer the user in plain text."
	if internet {
		prompt += "\nYou can search and open web pages. For current information, recent events, live data, websites, or facts that may have changed since training, use internet.search and internet.open before answering. Do not tell the user to search the web themselves."
	}
	return prompt
}

// MergeMissingTools appends tools that are absent. Existing policies stay as they are.
func MergeMissingTools(existing, additions []contracts.ToolPolicy) []contracts.ToolPolicy {
	seen := map[string]struct{}{}
	out := append([]contracts.ToolPolicy(nil), existing...)
	for _, tool := range existing {
		seen[tool.ToolID] = struct{}{}
	}
	for _, tool := range additions {
		if tool.ToolID == "" {
			continue
		}
		if _, ok := seen[tool.ToolID]; ok {
			continue
		}
		out = append(out, tool)
		seen[tool.ToolID] = struct{}{}
	}
	return out
}
