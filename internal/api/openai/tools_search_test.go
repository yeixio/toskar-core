package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/auth"
)

// manyTools are an agent's: its own few, then many from MCP servers, each
// with a long description, as Claude Code sends them.
func manyTools() []json.RawMessage {
	tool := func(name, about string) json.RawMessage {
		return json.RawMessage(fmt.Sprintf(`{"type":"function","function":{"name":%q,"description":%q,"parameters":{"type":"object"}}}`, name, about))
	}
	padding := strings.Repeat("Details about options and formats. ", 60)
	out := []json.RawMessage{
		tool("Read", "Read a file from the project"),
		tool("Bash", "Run a shell command"),
	}
	for i := range 40 {
		out = append(out, tool(fmt.Sprintf("mcp__docs__tool_%d", i), "A document tool. "+padding))
	}
	out = append(out, tool("mcp__calendar__create_event", "Create a calendar event with a title and a time. "+padding))
	return out
}

// Tools the conversation called stay, then those matching the request, in
// the agent's order, within the budget (#843).
func TestChooseTools(t *testing.T) {
	tools := readTools(manyTools())
	kept, deferred := chooseTools(tools, map[string]bool{"mcp__docs__tool_7": true}, "read main.go", 2000)
	names := map[string]bool{}
	spent := 0
	for _, k := range kept {
		names[k.name] = true
		spent += k.tokens
	}
	if !names["Read"] || !names["mcp__docs__tool_7"] || names["mcp__calendar__create_event"] {
		t.Errorf("kept %v", names)
	}
	if len(kept)+len(deferred) != len(tools) || spent > 2000+tools[9].tokens {
		t.Errorf("kept %d, deferred %d, %d tokens", len(kept), len(deferred), spent)
	}
	if got := search(deferred, "create a calendar event"); len(got) == 0 || got[0].name != "mcp__calendar__create_event" {
		t.Errorf("search: %+v", got)
	}
	if got := search(deferred, "zzz nothing"); len(got) != 0 {
		t.Errorf("search for nothing: %d", len(got))
	}
}

// A raw request whose tools take more than their share of the window gets
// tools_search: the model searches, Toskar adds what it found and asks
// again, and the agent sees only the call to its own tool, streamed as it
// asked (#843). A request with few tools goes as it came.
func TestToolsSearch(t *testing.T) {
	var rounds []map[string]any
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(raw, &req)
		rounds = append(rounds, req)
		tools, _ := req["tools"].([]any)
		has := func(name string) bool {
			for _, tl := range tools {
				if strings.Contains(fmt.Sprint(tl), name) {
					return true
				}
			}
			return false
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case req["stream"] == true:
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n")
		case has("mcp__calendar__create_event"):
			fmt.Fprint(w, `{"choices":[{"finish_reason":"tool_calls","message":{"content":"","tool_calls":[{"id":"c2","type":"function","function":{"name":"mcp__calendar__create_event","arguments":"{\"title\":\"Standup\"}"}}]}}],"usage":{"prompt_tokens":900,"completion_tokens":20}}`)
		default:
			fmt.Fprint(w, `{"choices":[{"finish_reason":"tool_calls","message":{"content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"tools_search","arguments":"{\"query\":\"create a calendar event\"}"}}]}}],"usage":{"prompt_tokens":800,"completion_tokens":15}}`)
		}
	}))
	defer model.Close()

	h := testHandler(t)
	h.Permissions = func(r *http.Request) (auth.APIKeyPermissions, context.Context, error) {
		return auth.APIKeyPermissions{Raw: true}, r.Context(), nil
	}
	h.Raw = func(context.Context, string, int) (string, int, func(), error) {
		return model.URL, 8192, func() {}, nil
	}
	tools, _ := json.Marshal(manyTools())
	body := `{"model":"raw:m","stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"Put a standup on my schedule at 9"}],"tools":` + string(tools) + `}`
	rec := httptest.NewRecorder()
	h.HandleChatCompletions(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
	out := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(out, "mcp__calendar__create_event") || strings.Contains(out, "tools_search") || !strings.Contains(out, "data: [DONE]") {
		t.Fatalf("%d %s", rec.Code, out)
	}
	if !strings.Contains(out, `"prompt_tokens":1700`) || !strings.Contains(out, `"model":"raw:m"`) {
		t.Errorf("usage over both rounds, and the name: %s", out)
	}
	if len(rounds) != 2 {
		t.Fatalf("%d rounds", len(rounds))
	}
	first, _ := rounds[0]["tools"].([]any)
	if len(first) >= len(manyTools()) || !strings.Contains(fmt.Sprint(first), "tools_search") || rounds[0]["stream"] != false {
		t.Errorf("first round: %d tools, stream %v", len(first), rounds[0]["stream"])
	}
	msgs, _ := rounds[1]["messages"].([]any)
	if len(msgs) != 3 || !strings.Contains(fmt.Sprint(msgs[2]), "mcp__calendar__create_event") {
		t.Errorf("second round's messages: %v", msgs)
	}

	// When the request names what a tool does, relevance keeps the tool,
	// and the model needs no search.
	rounds = nil
	rec = httptest.NewRecorder()
	h.HandleChatCompletions(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(strings.Replace(body, "my schedule", "my calendar", 1))))
	if len(rounds) != 1 || !strings.Contains(rec.Body.String(), "mcp__calendar__create_event") {
		t.Errorf("named in the request: %d rounds, %s", len(rounds), rec.Body)
	}

	// A few tools go as they came, streamed by the model.
	rounds = nil
	rec = httptest.NewRecorder()
	h.HandleChatCompletions(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"raw:m","stream":true,"messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"Read","parameters":{"type":"object"}}}]}`)))
	if len(rounds) != 1 || rounds[0]["stream"] != true || strings.Contains(fmt.Sprint(rounds[0]["tools"]), "tools_search") {
		t.Errorf("few tools: %v", rounds)
	}
}
