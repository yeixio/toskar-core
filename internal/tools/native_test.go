package tools

import (
	"reflect"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// The native tools are exactly the ones the prompt lists, with their
// schemas, so a model offered both sees the same tools twice, not two sets.
func TestNativeFromPromptMatchesThePrompt(t *testing.T) {
	profile := contracts.AIProfile{Tools: []contracts.ToolPolicy{
		{ToolID: "terminal", Policy: "ask"},
		{ToolID: "filesystem.read", Policy: "allow"},
		{ToolID: "internet.search", Policy: "allow"},
	}}
	system := "Be helpful.\n" + PromptFor(profile)
	native := NativeFromPrompt(system)
	enabled := Enabled(profile, nil)
	if len(native) != len(enabled) || len(native) == 0 {
		t.Fatalf("%d native tools for %d in the prompt", len(native), len(enabled))
	}
	for i, def := range enabled {
		fn := native[i]["function"].(map[string]any)
		if fn["name"] != def.ID || fn["description"] != def.Description {
			t.Errorf("tool %d: %v, want %s: %s", i, fn, def.ID, def.Description)
		}
	}
	read := nativeOf(t, native, "filesystem.read")
	want := map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}}
	if !reflect.DeepEqual(read["parameters"], want) {
		t.Errorf("filesystem.read parameters = %v", read["parameters"])
	}
	if NativeFromPrompt("Be helpful.") != nil {
		t.Error("tools from a prompt that lists none")
	}
}

func nativeOf(t *testing.T, native []map[string]any, name string) map[string]any {
	t.Helper()
	for _, tool := range native {
		if fn := tool["function"].(map[string]any); fn["name"] == name {
			return fn
		}
	}
	t.Fatalf("no %s", name)
	return nil
}

func TestNativeParameters(t *testing.T) {
	short := nativeParameters([]byte(`{"repo":"owner/name","number":"integer","submit":"boolean"}`))
	props := short["properties"].(map[string]any)
	if props["number"].(map[string]any)["type"] != "integer" || props["submit"].(map[string]any)["type"] != "boolean" {
		t.Errorf("types: %v", props)
	}
	if repo := props["repo"].(map[string]any); repo["type"] != "string" || repo["description"] != "owner/name" {
		t.Errorf("a shape is a string described by it: %v", repo)
	}
	// An MCP tool's full JSON Schema is kept as it is.
	full := nativeParameters([]byte(`{"type":"object","properties":{"q":{"type":"string","minLength":1}},"required":["q"]}`))
	if full["required"] == nil || full["properties"].(map[string]any)["q"].(map[string]any)["minLength"] != 1.0 {
		t.Errorf("full schema changed: %v", full)
	}
}

// A native call reads as a call in Toskar's own protocol.
func TestCallTextParses(t *testing.T) {
	out := ParseModelOutput(CallText("terminal", `{"command":"go test ./..."}`))
	if out.Call == nil || out.Call.ID != "terminal" || out.Call.Args["command"] != "go test ./..." || out.Text != "" {
		t.Fatalf("got %+v", out)
	}
	// Arguments that aren't an object become none, rather than broken JSON:
	// the call's own checks then say what's missing.
	if got := CallText("filesystem.read", "not json"); got != `{"tool_call":{"args":{},"id":"filesystem.read"}}` {
		t.Fatalf("bad arguments: %s", got)
	}
}
