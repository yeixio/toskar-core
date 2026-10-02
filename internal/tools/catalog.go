package tools

import (
	"sort"
	"strings"
	"sync"

	"github.com/yeixio/yggdrasil-core/pkg/contracts"
)

// Capability IDs are the user-facing groups. Individual tools stay the model API.
const (
	CapInternet = "internet"
	CapFiles    = "files"
	CapShell    = "shell"
	CapGit      = "git"
	// CapCode runs code in a sandbox (Gungnir §20).
	CapCode = "code"
)

// Definition is one registered tool: built in, from a connected service,
// or from an MCP tool source.
type Definition struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Capability    string `json:"capability"`
	Source        string `json:"source"`
	Schema        string `json:"schema"`
	DefaultPolicy string `json:"default_policy"`
	Risk          string `json:"risk"` // read | create | write
	// Prefetch marks a connected service's read tool that Yggdrasil calls,
	// with no arguments, before the model answers a message about that
	// service, as it looks up the web first for current questions.
	Prefetch bool `json:"prefetch,omitempty"`
	// Cues are words that show a message is about this tool's service,
	// such as a connected home's device names.
	Cues []string `json:"cues,omitempty"`
	// Always offers the tool with every request, not only ones about its
	// service: a tool source the person set to always be available.
	Always bool `json:"always,omitempty"`

	// The rest describe the tool for the registry (Gungnir §7). Each is
	// optional; Describe works out what a definition leaves unset.
	Version int `json:"-"`
	// InputSchema is a full JSON Schema of the arguments, when the short
	// Schema above is not enough.
	InputSchema string   `json:"-"`
	Outputs     []string `json:"-"`
	// Level overrides the permission level worked out from risk and source.
	Level int `json:"-"`
	// Execution is local, remote, or either.
	Execution string `json:"-"`
	// Runtime is a runtime the tool needs, such as python.
	Runtime     string `json:"-"`
	GPU         bool   `json:"-"`
	Credentials bool   `json:"-"`
	// Progress marks a tool that reports progress while it runs.
	Progress bool `json:"-"`
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
		{ID: "files.create", Name: "Create File", Description: "Create a file the user can download: a Word document (.docx) or PDF (.pdf) written in Markdown, a document (.md, .txt, .html), data (.json, .csv), a spreadsheet (.xlsx, given as CSV text; a line \"## Sheet: Name\" starts another sheet, and a cell starting with = is a formula), or code. Use it when the user asks for a file, a spreadsheet, a PDF, or a document.", Capability: CapFiles, Source: "builtin", Schema: `{"name":"string","content":"string"}`, DefaultPolicy: PolicyAllow, Risk: RiskCreate},
		{ID: "spreadsheet.analyze", Name: "Analyze Spreadsheet", Description: "Summarize a spreadsheet (.xlsx, .csv) attached to or made in this chat: each sheet's rows, and each column's type, count, minimum, maximum, average, and total, or its most common values, with the first rows. Use it to answer questions about a spreadsheet's whole contents.", Capability: CapFiles, Source: "builtin", Schema: `{"file":"string","sheet":"string"}`, DefaultPolicy: PolicyAllow, Risk: RiskRead},
		{ID: "code.execute", Name: "Run Code", Description: "Run Python in a sandbox for calculations, data analysis, and charts, with numpy, pandas, and matplotlib. It has no network and sees only files you list from this chat (\"files\": [\"sales.xlsx\"]), read from its working folder. Print results; files it saves there (.png, .csv, .xlsx, .pdf, and so on) are attached to the answer. Use matplotlib's savefig for charts.", Capability: CapCode, Source: "builtin", Schema: `{"code":"string","files":"array"}`, DefaultPolicy: PolicyAsk, Risk: RiskWrite,
			Level: LevelHighImpact, Runtime: "python", Outputs: []string{OutputText, OutputFile}},
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
	id = Canonical(id)
	for _, def := range Catalog() {
		if def.ID == id {
			return def, true
		}
	}
	return Definition{}, false
}

// PolicyForProfile returns the stored policy, or deny when the profile does not list the tool.
func PolicyForProfile(profile contracts.AIProfile, toolID string) string {
	toolID = Canonical(toolID)
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

// Connected tools come from services the user connected (spec §32) and
// from MCP tool sources. They change while the daemon runs, so they live
// beside the built-in catalog.
var (
	connectedMu sync.RWMutex
	connected   = map[string][]Definition{}
)

// SetConnected replaces the tools a connected service provides; nil
// removes them.
func SetConnected(source string, defs []Definition) {
	connectedMu.Lock()
	defer connectedMu.Unlock()
	if len(defs) == 0 {
		delete(connected, source)
		return
	}
	connected[source] = append([]Definition(nil), defs...)
}

// ConnectedDefinitions lists the tools of every connected service, sorted by id.
func ConnectedDefinitions() []Definition {
	connectedMu.RLock()
	defer connectedMu.RUnlock()
	var out []Definition
	for _, defs := range connected {
		out = append(out, defs...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Catalog is every tool: built in, then connected services.
func Catalog() []Definition {
	return append(BuiltinCatalog(), ConnectedDefinitions()...)
}

// WithConnected gives a profile the tools of connected services it does not
// list yet, at each tool's default policy: reading allowed, changes asked
// first. A policy the profile already sets for one, such as deny, stays.
func WithConnected(profile contracts.AIProfile) contracts.AIProfile {
	defs := ConnectedDefinitions()
	if len(defs) == 0 {
		return profile
	}
	add := make([]contracts.ToolPolicy, 0, len(defs))
	for _, def := range defs {
		add = append(add, contracts.ToolPolicy{ToolID: def.ID, Policy: def.DefaultPolicy})
	}
	profile.Tools = MergeMissingTools(profile.Tools, add)
	return profile
}

// Enabled reports tools this profile will actually expose to a model.
func Enabled(profile contracts.AIProfile, globallyDisabled map[string]struct{}) []Definition {
	var out []Definition
	for _, def := range Catalog() {
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
