package tools

import (
	"encoding/json"
	"strings"
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

// Every array gets the items it holds, any value when it doesn't say, in a
// short schema and at any depth of a full one, with every key where it was
// (#1140): gpt-oss's chat template stopped llama-server on an array
// without them.
func TestNativeArraysHaveItems(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`{"rows":"array","path":"string"}`, `{"type":"object","properties":{"rows":{"items":{},"type":"array"},"path":{"type":"string"}}}`},
		{`{"type":"object","properties":{"z":{"type":"array"},"a":{"type":"object","properties":{"tags":{"description":"<b> & more","type":"array"}}}}}`,
			`{"type":"object","properties":{"z":{"type":"array","items":{}},"a":{"type":"object","properties":{"tags":{"description":"<b> & more","type":"array","items":{}}}}}}`},
		{`{"type":"object","properties":{"ids":{"type":"array","items":{"type":"integer"},"maxItems":3}}}`,
			`{"type":"object","properties":{"ids":{"type":"array","items":{"type":"integer"},"maxItems":3}}}`},
	} {
		got := string(nativeParameters(json.RawMessage(tc.in)))
		if got != tc.want {
			// The short form's keys come from a map, so compare it as JSON.
			var g, w any
			_ = json.Unmarshal([]byte(got), &g)
			_ = json.Unmarshal([]byte(tc.want), &w)
			gj, _ := json.Marshal(g)
			wj, _ := json.Marshal(w)
			if strings.HasPrefix(tc.in, `{"type"`) || string(gj) != string(wj) {
				t.Errorf("%s:\n got %s\nwant %s", tc.in, got, tc.want)
			}
		}
	}
}

// Every built-in tool's parameters, as gpt-oss is offered them, have items
// on every array.
func TestBuiltinToolsArraysHaveItems(t *testing.T) {
	for _, d := range Catalog() {
		schema := d.InputSchema
		if schema == "" {
			schema = d.Schema
		}
		if schema == "" {
			continue
		}
		var v any
		if err := json.Unmarshal(nativeParameters(json.RawMessage(schema)), &v); err != nil {
			t.Errorf("%s: %v", d.ID, err)
			continue
		}
		var check func(any, string)
		check = func(v any, at string) {
			switch v := v.(type) {
			case map[string]any:
				if _, ok := v["items"]; v["type"] == "array" && !ok {
					t.Errorf("%s: an array without items at %s", d.ID, at)
				}
				for k, c := range v {
					check(c, at+"."+k)
				}
			case []any:
				for _, c := range v {
					check(c, at+"[]")
				}
			}
		}
		check(v, "")
	}
}
