package tools

import (
	"strings"
	"testing"
)

func TestStructuredToolCallIsNotVisible(t *testing.T) {
	out := ParseModelOutput(`{"tool_call":{"id":"internet.search","args":{"query":"Juneau weather"}}}`)
	if out.Call == nil || out.Call.ID != "internet.search" || out.Call.Args["query"] != "Juneau weather" {
		t.Fatalf("call=%+v", out.Call)
	}
	if strings.Contains(out.Text, "tool_call") || strings.Contains(out.Text, "Juneau") {
		t.Fatalf("visible %q", out.Text)
	}
}

func TestTextEncodedToolCallKeepsProse(t *testing.T) {
	out := ParseModelOutput("Let me check that.\n" + `{"tool_call":{"id":"internet.search","args":{"query":"Juneau weather"}}}`)
	if out.Call == nil || out.Call.ID != "internet.search" {
		t.Fatalf("call=%+v", out.Call)
	}
	if out.Text != "Let me check that." || strings.Contains(out.Text, "tool_call") {
		t.Fatalf("visible %q", out.Text)
	}
}

func TestOrdinaryJSONStaysVisible(t *testing.T) {
	sample := "Here is the object:\n{\"name\":\"Ada\",\"year\":1815}"
	out := ParseModelOutput(sample)
	if out.Call != nil {
		t.Fatalf("ordinary JSON was a tool call: %+v", out.Call)
	}
	if !strings.Contains(out.Text, `"name"`) || !strings.Contains(out.Text, "1815") {
		t.Fatalf("visible %q", out.Text)
	}
}

func TestCodeFenceJSONStaysVisible(t *testing.T) {
	sample := "Example:\n```json\n{\"query\":\"not a tool\"}\n```"
	out := ParseModelOutput(sample)
	if out.Call != nil || !strings.Contains(out.Text, `"query"`) {
		t.Fatalf("out=%+v text=%q", out.Call, out.Text)
	}
}

func TestMalformedToolCallDoesNotExecute(t *testing.T) {
	out := ParseModelOutput(`{"tool_call":{"id":"internet.search","args":`)
	if out.Call != nil {
		t.Fatalf("executed partial call %+v", out.Call)
	}
	if !out.Malformed || strings.Contains(out.Text, "tool_call") {
		t.Fatalf("malformed=%v text=%q", out.Malformed, out.Text)
	}
}

func TestUnknownToolAndPartialArgsAreRejected(t *testing.T) {
	unknown := ParseModelOutput(`{"tool_call":{"id":"weather_check","args":{"city":"Juneau"}}}`)
	if unknown.Call != nil || strings.Contains(unknown.Text, "weather_check") {
		t.Fatalf("unknown call leaked: %+v %q", unknown.Call, unknown.Text)
	}
	missing := ParseModelOutput(`{"tool_call":{"id":"internet.search","args":{}}}`)
	if missing.Call != nil {
		t.Fatal("missing query was executed")
	}
}

func TestToolFailureEnvelopeIsHidden(t *testing.T) {
	raw := "Juneau is currently cloudy.\n" + `{"tool_result":{"tool_id":"internet.search","ok":false,"error":"stack trace line"}}`
	out := ParseModelOutput(raw)
	if strings.Contains(out.Text, "tool_result") || strings.Contains(out.Text, "stack trace") {
		t.Fatalf("visible %q", out.Text)
	}
	if !strings.Contains(out.Text, "cloudy") {
		t.Fatalf("visible %q", out.Text)
	}
}

func TestToolNarrationIsRemovedWhenACallIsPresent(t *testing.T) {
	raw := `I will now use the web tool. {"tool_call":{"id":"internet.search","args":{"query":"Juneau weather"}}}`
	out := ParseModelOutput(raw)
	if out.Call == nil {
		t.Fatal("expected a call")
	}
	if strings.Contains(strings.ToLower(out.Text), "web tool") || strings.Contains(out.Text, "tool_call") {
		t.Fatalf("visible %q", out.Text)
	}
}

func TestTaggedAndFunctionCallFormats(t *testing.T) {
	tagged := ParseModelOutput(`<tool_call>{"id":"internet.open","args":{"url":"https://weather.gov"}}</tool_call>`)
	if tagged.Call == nil || tagged.Call.ID != "internet.open" || tagged.Text != "" {
		t.Fatalf("tagged %+v %q", tagged.Call, tagged.Text)
	}
	fn := ParseModelOutput(`function_call(git.status, {})`)
	if fn.Call == nil || fn.Call.ID != "git.status" || strings.Contains(fn.Text, "function_call") {
		t.Fatalf("function %+v %q", fn.Call, fn.Text)
	}
}

// Calls as Qwen 3.5 wrote them, with its own label for the call in id, or
// the arguments as a bare string, run as the tool they name (#1124).
func TestCallsWithALabelForIDRunAsTheTool(t *testing.T) {
	for _, tc := range []struct{ raw, id, arg, value string }{
		{`{"tool_call":{"id":"terminal_1","args":{"command":"go test ./..."}}}`, "terminal", "command", "go test ./..."},
		{`{"tool_call":{"id":"run_test_001","args":{"command":"go test ./..."}}}`, "terminal", "command", "go test ./..."},
		{`{"tool_call":{"id":"terminal_1","args":"du -ah --max-depth=2 | sort -hr"}}`, "terminal", "command", "du -ah --max-depth=2 | sort -hr"},
		{`{"tool_call":{"id":"read_readme","args":{"path":"README.md"}}}`, "filesystem.read", "path", "README.md"},
		{`{"tool_call":{"id":"filesystem.read-2","args":{"path":"README.md"}}}`, "filesystem.read", "path", "README.md"},
	} {
		out := ParseModelOutput(tc.raw)
		if out.Call == nil || out.Call.ID != tc.id || out.Call.Args[tc.arg] != tc.value || len(out.Call.Args) != 1 {
			t.Errorf("%s: call=%+v malformed=%v", tc.raw, out.Call, out.Malformed)
		}
	}
}

// What a call asks for is never guessed: arguments that fit several tools,
// or none, and a bare string for a tool with no single argument, stay
// invalid, and the model is asked again.
func TestUnclearCallsStayInvalid(t *testing.T) {
	for _, raw := range []string{
		`{"tool_call":{"id":"look_it_up","args":{"query":"Juneau weather"}}}`,
		`{"tool_call":{"id":"fs_search_1","args":{"query":"*","path":"./"}}}`,
		`{"tool_call":{"id":"run_tests","args":"go test ./..."}}`,
		`{"tool_call":{"id":"filesystem.write_1","args":"hello"}}`,
		`{"tool_call":{"id":"terminal","args":"   "}}`,
	} {
		out := ParseModelOutput(raw)
		if out.Call != nil || !out.Malformed {
			t.Errorf("%s: call=%+v malformed=%v", raw, out.Call, out.Malformed)
		}
	}
}
