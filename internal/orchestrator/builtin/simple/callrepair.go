package simple

import (
	"encoding/json"
	"strings"

	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// numberedCall reads a tool call whose id isn't a tool, such as
// {"tool_call":{"id":"1","args":{"query":"…"}}}: a small model numbering
// its calls as OpenAI's call ids do, where Toskar's format names the tool
// (#1213). Qwen 3.5 4B did it three times running, each refused, and the
// turn ended with no answer.
func numberedCall(content string) (string, map[string]any, bool) {
	start, end := strings.IndexByte(content, '{'), strings.LastIndexByte(content, '}')
	if start < 0 || end <= start {
		return "", nil, false
	}
	var v struct {
		ToolCall *struct {
			ID   string         `json:"id"`
			Args map[string]any `json:"args"`
		} `json:"tool_call"`
	}
	if json.Unmarshal([]byte(content[start:end+1]), &v) != nil || v.ToolCall == nil || len(v.ToolCall.Args) == 0 {
		return "", nil, false
	}
	if _, known := tools.Lookup(tools.Canonical(v.ToolCall.ID)); known {
		return "", nil, false
	}
	return v.ToolCall.ID, v.ToolCall.Args, true
}

// toolsFitting are the offered tools whose arguments include every one of
// args'.
func toolsFitting(args map[string]any, profile contracts.AIProfile) []string {
	var out []string
	for _, def := range tools.Enabled(profile, nil) {
		var schema map[string]any
		if json.Unmarshal([]byte(def.Schema), &schema) != nil {
			continue
		}
		fits := true
		for k := range args {
			if _, ok := schema[k]; !ok {
				fits = false
				break
			}
		}
		if fits {
			out = append(out, def.ID)
		}
	}
	return out
}

// repairedCall is a numbered call that only one offered tool fits, as that
// tool's call.
func repairedCall(content string, profile contracts.AIProfile) (*tools.ModelCall, bool) {
	_, args, ok := numberedCall(content)
	if !ok {
		return nil, false
	}
	if fit := toolsFitting(args, profile); len(fit) == 1 {
		return &tools.ModelCall{ID: fit[0], Args: args}, true
	}
	return nil, false
}

// numberedCallNote shows a model that numbered its call the same call with
// each tool it could mean, so it picks one rather than numbering again.
func numberedCallNote(content string, profile contracts.AIProfile) (string, bool) {
	id, args, ok := numberedCall(content)
	if !ok {
		return "", false
	}
	fit := toolsFitting(args, profile)
	if len(fit) == 0 {
		return "", false
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return "", false
	}
	var examples []string
	for _, tool := range fit {
		examples = append(examples, `{"tool_call":{"id":"`+tool+`","args":`+string(raw)+`}}`)
	}
	return "That tool call was not run: \"" + id + "\" is not a tool. The id is the name of the tool to run, not a number for the call. Reply with one of these, whichever tool you mean, or answer in plain text:\n" + strings.Join(examples, "\n"), true
}
