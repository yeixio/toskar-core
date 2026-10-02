package tools

import (
	"encoding/json"
	"strings"
)

// Permission levels (Gungnir §10). They describe what a tool can affect, so
// people and policies can reason about it without knowing the tool.
const (
	// LevelLocal is a low-risk capability on this computer: reading local
	// files, or making a file in Yggdrasil's own store.
	LevelLocal = 1
	// LevelReadExternal reads data from outside: the web, a connected
	// service, an MCP tool source.
	LevelReadExternal = 2
	// LevelChange changes state: files in the workspace, a repository, a
	// connected service.
	LevelChange = 3
	// LevelHighImpact runs arbitrary commands or code.
	LevelHighImpact = 4
)

var levelNames = map[int]string{
	LevelLocal:        "Low risk, on this computer",
	LevelReadExternal: "Reads outside data",
	LevelChange:       "Changes things",
	LevelHighImpact:   "Runs commands or code",
}

// Outputs a tool can return (§34).
const (
	OutputText  = "text"
	OutputFile  = "file"
	OutputImage = "image"
	OutputAudio = "audio"
)

// Requirements are what a tool needs to run (§7).
type Requirements struct {
	Network     bool   `json:"network"`
	Filesystem  bool   `json:"filesystem"`
	Credentials bool   `json:"credentials"`
	Runtime     string `json:"runtime,omitempty"`
	GPU         bool   `json:"gpu,omitempty"`
}

// Supports says how a long-running tool can be followed (§33).
type Supports struct {
	Progress bool `json:"progress"`
	// Cancel is true for every tool: stopping a turn ends its tool calls.
	Cancel bool `json:"cancel"`
}

// Descriptor is the common description every tool advertises, whatever it
// comes from: built in, a connected service, or an MCP tool source (§7).
type Descriptor struct {
	Definition
	Version int `json:"version"`
	// InputSchema is a JSON Schema of the tool's arguments.
	InputSchema  map[string]any `json:"input_schema"`
	Outputs      []string       `json:"outputs"`
	Level        int            `json:"level"`
	LevelName    string         `json:"level_name"`
	Execution    string         `json:"execution"`
	Requirements Requirements   `json:"requirements"`
	Supports     Supports       `json:"supports"`
	// TimeoutSeconds is the longest one call may run.
	TimeoutSeconds int `json:"timeout_seconds"`
	// Provider is what implements the tool: builtin, connector:<service>,
	// or mcp:<source>.
	Provider string `json:"provider"`
}

// Describe returns a tool's descriptor. Fields a definition does not set are
// worked out from its capability, risk, and source.
func Describe(def Definition) Descriptor {
	d := Descriptor{
		Definition:     def,
		Version:        def.Version,
		InputSchema:    inputSchema(def),
		Outputs:        def.Outputs,
		Level:          Level(def),
		Execution:      def.Execution,
		Supports:       Supports{Progress: def.Progress, Cancel: true},
		TimeoutSeconds: int(Timeout(def.ID).Seconds()),
		Provider:       def.Source,
	}
	if d.Version == 0 {
		d.Version = 1
	}
	if len(d.Outputs) == 0 {
		d.Outputs = []string{OutputText}
		if def.Risk == RiskCreate {
			d.Outputs = []string{OutputFile}
		}
	}
	if d.Execution == "" {
		d.Execution = "local"
	}
	if d.Provider == "" {
		d.Provider = "builtin"
	}
	d.LevelName = levelNames[d.Level]
	d.Requirements = Requirements{
		Network:     def.Capability == CapInternet || external(def) || def.ID == "git.push",
		Filesystem:  def.Capability == CapFiles || def.Capability == CapGit || def.Capability == CapShell,
		Credentials: strings.HasPrefix(def.Source, "connector:") || def.Credentials,
		Runtime:     def.Runtime,
		GPU:         def.GPU,
	}
	return d
}

// external reports a tool that talks to a service outside this computer.
func external(def Definition) bool {
	return strings.HasPrefix(def.Source, "connector:") || strings.HasPrefix(def.Source, "mcp:")
}

// Level is a tool's permission level (§10).
func Level(def Definition) int {
	if def.Level > 0 {
		return def.Level
	}
	switch {
	case def.Capability == CapShell:
		return LevelHighImpact
	case def.Risk == RiskWrite:
		return LevelChange
	case def.Risk == RiskCreate:
		return LevelLocal
	case def.Capability == CapInternet || external(def):
		return LevelReadExternal
	default:
		return LevelLocal
	}
}

// inputSchema is a JSON Schema for a tool's arguments: its own, when it has
// one, or one built from its short schema such as {"query":"string"}.
func inputSchema(def Definition) map[string]any {
	if def.InputSchema != "" {
		var s map[string]any
		if json.Unmarshal([]byte(def.InputSchema), &s) == nil {
			return s
		}
	}
	props := map[string]any{}
	if s := argSchema(def); s != nil {
		for name, p := range s.Properties {
			props[name] = map[string]any{"type": p.Type}
		}
	}
	return map[string]any{"type": "object", "properties": props}
}
