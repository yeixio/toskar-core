package tools

import (
	"encoding/json"
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
	if got := string(read["parameters"].(json.RawMessage)); got != `{"type":"object","properties":{"path":{"type":"string"}}}` {
		t.Errorf("filesystem.read parameters = %s", got)
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
	short := string(nativeParameters([]byte(`{"repo":"owner/name","number":"integer","submit":"boolean"}`)))
	want := `{"type":"object","properties":{"repo":{"description":"owner/name","type":"string"},"number":{"type":"integer"},"submit":{"type":"boolean"}}}`
	if short != want {
		t.Errorf("short form:\n got %s\nwant %s", short, want)
	}
	// The properties keep the prompt's order: llama.cpp holds a call to it,
	// and gpt-oss's write calls lost their content with path after content
	// (#1112).
	if write := string(nativeParameters([]byte(`{"path":"string","content":"string"}`))); write != `{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}}}` {
		t.Errorf("filesystem.write: %s", write)
	}
	// An MCP tool's full JSON Schema is kept as it is, byte for byte.
	schema := `{"type":"object","properties":{"z":{"type":"string","minLength":1},"a":{"type":"integer"}},"required":["z"]}`
	if full := string(nativeParameters([]byte(" " + schema + "\n"))); full != schema {
		t.Errorf("full schema changed: %s", full)
	}
	if empty := string(nativeParameters([]byte(`not json`))); empty != `{"type":"object","properties":{}}` {
		t.Errorf("no schema: %s", empty)
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
