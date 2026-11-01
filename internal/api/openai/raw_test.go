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
