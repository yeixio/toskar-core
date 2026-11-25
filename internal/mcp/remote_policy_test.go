package mcp

import (
	"testing"

	"github.com/yeixio/toskar-core/internal/tools"
)

// A web server's own "read only" hint isn't trusted for the default: its
// tools ask until the person allows them. Gallery servers and servers on
// this computer keep reading without asking (#1209).
func TestRemoteServersAskByDefault(t *testing.T) {
	yes := true
	list := []RemoteTool{
		{Name: "get_notes", Annotations: &ToolAnnotations{ReadOnlyHint: &yes}},
		{Name: "delete_note"},
	}
	defs := func(spec Spec) map[string]tools.Definition {
		m := &Manager{}
		out := map[string]tools.Definition{}
		for _, d := range m.definitionsLocked(&server{id: "notes", spec: spec, tools: list}) {
			out[d.ID] = d
		}
		return out
	}

	web := defs(Spec{Name: "Notes", URL: "https://notes.example/mcp"})
	if d := web["notes.get_notes"]; d.DefaultPolicy != tools.PolicyAsk || d.Risk != tools.RiskRead {
		t.Fatalf("a web server's read tool: policy %s, risk %s", d.DefaultPolicy, d.Risk)
	}
	if d := web["notes.delete_note"]; d.DefaultPolicy != tools.PolicyAsk {
		t.Fatalf("a web server's write tool: %s", d.DefaultPolicy)
	}

	gallery := defs(Spec{Name: "Notes", URL: "https://notes.example/mcp", Preset: "notes"})
	if d := gallery["notes.get_notes"]; d.DefaultPolicy != tools.PolicyAllow {
		t.Fatalf("a gallery server's read tool: %s", d.DefaultPolicy)
	}
	local := defs(Spec{Name: "Notes", Command: "notes-mcp"})
	if d := local["notes.get_notes"]; d.DefaultPolicy != tools.PolicyAllow {
		t.Fatalf("a local server's read tool: %s", d.DefaultPolicy)
	}
}
