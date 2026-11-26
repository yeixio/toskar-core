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
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// A raw:<model> request goes to the model's llama-server as it came, tools
// and all, less Toskar's own object, and comes back with the model's
// tool_calls, named as asked (#843).
func TestRawModel(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		got = nil
		_ = json.Unmarshal(raw, &got)
		if got["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"model\":\"gpt-3.5-turbo\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"read_file\"}}]}}]}\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"model":"gpt-3.5-turbo","choices":[{"message":{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"main.go\"}"}}]},"finish_reason":"tool_calls"}]}`)
	}))
	defer server.Close()

	h := testHandler(t)
	perms := auth.APIKeyPermissions{Raw: true}
	h.Permissions = func(r *http.Request) (auth.APIKeyPermissions, context.Context, error) { return perms, r.Context(), nil }
	var asked int
	h.Raw = func(_ context.Context, model string, need int) (string, int, func(), error) {
		asked = need
		switch model {
		case "here":
			return server.URL, 32768, func() {}, nil
		case "huge":
			return "", 0, nil, fmt.Errorf("%w: the request is about 900000 tokens", ErrRawTooLong)
		}
		return "", 0, nil, ErrRawNotHere
	}
	h.RawModels = func(context.Context) []string { return []string{"here"} }
	call := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.HandleChatCompletions(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
		return rec
	}
	tools := `"tools":[{"type":"function","function":{"name":"read_file","parameters":{"type":"object"}}}],"tool_choice":"auto","stop":["</done>"]`

	// The answer comes back as the model gave it, its tool calls included.
	rec := call(`{"model":"raw:here","messages":[{"role":"user","content":"read main.go"},{"role":"tool","tool_call_id":"c0","content":"ok"}],` + tools + `,"toskar":{"memory":true}}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"read_file"`) || !strings.Contains(rec.Body.String(), `"model":"raw:here"`) {
		t.Fatalf("raw answer: %d %s", rec.Code, rec.Body)
	}
	if got["tools"] == nil || got["tool_choice"] != "auto" || got["stop"] == nil || got["toskar"] != nil {
		t.Errorf("sent to the model: %v", got)
	}
	if msgs, _ := got["messages"].([]any); len(msgs) != 2 {
		t.Errorf("the tool message wasn't passed on: %v", got["messages"])
	}
	if asked < answerRoom {
		t.Errorf("asked for a window of %d", asked)
	}

	// Streamed, each chunk is named as asked, and the stream still ends.
	rec = call(`{"model":"raw:here","stream":true,"messages":[{"role":"user","content":"hi"}],` + tools + `}`)
	if body := rec.Body.String(); rec.Code != http.StatusOK || !strings.Contains(body, `"model":"raw:here"`) || !strings.Contains(body, "data: [DONE]") || !strings.Contains(body, "tool_calls") {
		t.Fatalf("raw stream: %d %s", rec.Code, body)
	}

	for _, c := range []struct {
		name, body string
		status     int
		code       string
	}{
		{"not here", `{"model":"raw:elsewhere","messages":[{"role":"user","content":"hi"}]}`, http.StatusNotFound, "MODEL_NOT_FOUND"},
		{"too long", `{"model":"raw:huge","messages":[{"role":"user","content":"hi"}]}`, http.StatusBadRequest, "CONTEXT_TOO_LONG"},
		{"no model", `{"model":"raw:","messages":[{"role":"user","content":"hi"}]}`, http.StatusBadRequest, "INVALID_REQUEST"},
	} {
		if rec := call(c.body); rec.Code != c.status || !strings.Contains(rec.Body.String(), c.code) {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body)
		}
	}

	// A key needs raw access, and one pinned to a profile can't have it.
	for _, p := range []auth.APIKeyPermissions{{}, {Raw: true, Profile: "general"}} {
		perms = p
		if rec := call(`{"model":"raw:here","messages":[{"role":"user","content":"hi"}]}`); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "RAW_NOT_ALLOWED") {
			t.Errorf("%+v: %d %s", p, rec.Code, rec.Body)
		}
	}
}

// /v1/models lists raw:<model> only for a key with raw access (#843).
func TestRawModelsListed(t *testing.T) {
	h := testHandler(t)
	h.RawModels = func(context.Context) []string { return []string{"qwen2.5-7b-q4"} }
	for _, raw := range []bool{true, false} {
		perms := auth.APIKeyPermissions{Raw: raw}
		h.Permissions = func(r *http.Request) (auth.APIKeyPermissions, context.Context, error) { return perms, r.Context(), nil }
		rec := httptest.NewRecorder()
		h.HandleModels(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
		if listed := strings.Contains(rec.Body.String(), `"raw:qwen2.5-7b-q4"`); listed != raw {
			t.Errorf("raw %v: listed %v: %s", raw, listed, rec.Body)
		}
	}
}

// System turns partway, as Claude Code sends them, reach the model as one
// system message first: Qwen 3.5's template refuses any other (#1060).
func TestSystemFirst(t *testing.T) {
	in := `{"model":"m","messages":[{"role":"system","content":"Be terse."},{"role":"user","content":"hi"},{"role":"system","content":[{"type":"text","text":"Tools changed."}]},{"role":"assistant","content":"ok"}],"stream":true}`
	var got struct {
		Model    string           `json:"model"`
		Stream   bool             `json:"stream"`
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(systemFirst([]byte(in)), &got); err != nil {
		t.Fatal(err)
	}
	roles := []string{}
	for _, m := range got.Messages {
		roles = append(roles, m["role"].(string))
	}
	if strings.Join(roles, ",") != "system,user,assistant" || got.Messages[0]["content"] != "Be terse.\n\nTools changed." || got.Model != "m" || !got.Stream {
		t.Fatalf("got %+v", got)
	}
	// One system message first, or none, is left exactly as sent.
	for _, same := range []string{
		`{"messages":[{"role":"system","content":"a"},{"role":"user","content":"b"}]}`,
		`{"messages":[{"role":"user","content":"b"}]}`,
		`{"prompt":"x"}`,
	} {
		if out := string(systemFirst([]byte(same))); out != same {
			t.Errorf("%s became %s", same, out)
		}
	}
}

// A request with its own tools that asks for auto, a profile, or a model's
// id runs on the model the turn would use, as raw does, so the client gets
// its tool_calls; a key that can't drive models keeps Toskar's own tools
// (#1450).
func TestClientToolsGoToTheTurnsModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"model":"x","choices":[{"message":{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"HassTurnOn","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
	}))
	defer server.Close()

	h := testHandler(t)
	chat := &scriptChat{chunks: []pluginapi.ChatChunk{{Content: "Toskar's own answer", Done: true}}}
	h.Chat = chat
	perms := auth.APIKeyPermissions{Raw: true}
	h.Permissions = func(r *http.Request) (auth.APIKeyPermissions, context.Context, error) { return perms, r.Context(), nil }
	var ran string
	h.Raw = func(_ context.Context, model string, _ int) (string, int, func(), error) {
		ran = model
		return server.URL, 32768, func() {}, nil
	}
	var asked [2]string
	h.ToolModel = func(_ context.Context, profile, model string) (string, error) {
		asked = [2]string{profile, model}
		return "qwen3.5-4b-q4", nil
	}
	call := func(model, tools string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		body := `{"model":"` + model + `","messages":[{"role":"user","content":"turn on the kitchen light"}]` + tools + `}`
		h.HandleChatCompletions(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
		return rec
	}
	tools := `,"tools":[{"type":"function","function":{"name":"HassTurnOn","parameters":{"type":"object"}}}]`

	for model, want := range map[string][2]string{"auto": {"", "auto"}, "profile:general": {"general", ""}, "qwen3.5-4b-q4": {"", "qwen3.5-4b-q4"}} {
		ran = ""
		rec := call(model, tools)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "HassTurnOn") || !strings.Contains(rec.Body.String(), `"model":"`+model+`"`) {
			t.Fatalf("%s: %d %s", model, rec.Code, rec.Body)
		}
		if ran != "qwen3.5-4b-q4" || asked != want {
			t.Fatalf("%s: ran on %q, asked %v", model, ran, asked)
		}
	}

	// Without tools, Toskar answers as its own assistant.
	ran = ""
	if rec := call("auto", ""); !strings.Contains(rec.Body.String(), "Toskar's own answer") || ran != "" {
		t.Fatalf("no tools: ran %q, %s", ran, rec.Body)
	}
	// A key that can't drive models keeps Toskar's own tools.
	perms = auth.APIKeyPermissions{}
	if rec := call("auto", tools); !strings.Contains(rec.Body.String(), "Toskar's own answer") || ran != "" {
		t.Fatalf("no raw access: ran %q, %s", ran, rec.Body)
	}
}
