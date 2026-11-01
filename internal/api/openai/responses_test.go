package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/auth"
)

// stable masks a response's ids and time, so a golden file holds.
func stable(raw []byte) []byte {
	raw = regexp.MustCompile(`(resp|msg|fc)_[0-9a-f]{24}`).ReplaceAll(raw, []byte("${1}_ID"))
	return regexp.MustCompile(`"created_at":[0-9]+`).ReplaceAll(raw, []byte(`"created_at":1700000000`))
}

// A Codex-style agent's turn in the Responses format: instructions, a
// developer message, an earlier function call and its output, a picture,
// and a function tool, chosen by name.
const responsesRequestJSON = `{
  "model": "raw:qwen2.5-7b-q4",
  "instructions": "You are a coding agent.",
  "input": [
    {"role": "developer", "content": "Use the tools."},
    {"role": "user", "content": [{"type": "input_text", "text": "What's in main.go?"}]},
    {"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": "Let me look."}]},
    {"type": "function_call", "call_id": "call_1", "name": "read_file", "arguments": "{\"path\":\"main.go\"}"},
    {"type": "function_call_output", "call_id": "call_1", "output": "package main"},
    {"type": "reasoning", "summary": []},
    {"role": "user", "content": [{"type": "input_text", "text": "And this screenshot?"}, {"type": "input_image", "image_url": "data:image/png;base64,iVBORw0KGgo="}]}
  ],
  "tools": [{"type": "function", "name": "read_file", "description": "Read a file", "parameters": {"type": "object", "properties": {"path": {"type": "string"}}, "required": ["path"]}}],
  "tool_choice": {"type": "function", "name": "read_file"},
  "max_output_tokens": 512,
  "temperature": 0.2,
  "store": false
}`

// The Responses format reaches the model as Chat Completions, and the
// answer comes back as a response, whole or as its events (#843).
func TestResponses(t *testing.T) {
	var sent []byte
	model := fakeModel(t, &sent, chatAnswerJSON, chatStream)
	h := testHandler(t)
	perms := auth.APIKeyPermissions{Raw: true}
	h.Permissions = func(r *http.Request) (auth.APIKeyPermissions, context.Context, error) { return perms, r.Context(), nil }
	h.Raw = func(_ context.Context, id string, _ int) (string, func(), error) {
		if id != "qwen2.5-7b-q4" {
			return "", nil, ErrRawNotHere
		}
		return model.URL, func() {}, nil
	}
	call := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.HandleResponses(rec, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body)))
		return rec
	}

	rec := call(responsesRequestJSON)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	goldenFile(t, "responses/request.chat.json", indent(t, sent))
	goldenFile(t, "responses/answer.json", indent(t, stable(rec.Body.Bytes())))

	rec = call(strings.Replace(responsesRequestJSON, `"store": false`, `"store": false, "stream": true`, 1))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream: %d %s", rec.Code, rec.Body)
	}
	goldenFile(t, "responses/answer.stream.txt", stable(rec.Body.Bytes()))

	// A string input is a user message; the answer cut at the token limit
	// is incomplete.
	cut := fakeModel(t, &sent, `{"choices":[{"finish_reason":"length","message":{"content":"Partial"}}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`, "")
	h.Raw = func(context.Context, string, int) (string, func(), error) { return cut.URL, func() {}, nil }
	rec = call(`{"model":"qwen2.5-7b-q4","input":"Hi","max_output_tokens":1}`)
	if !strings.Contains(rec.Body.String(), `"status":"incomplete"`) || !strings.Contains(rec.Body.String(), `"reason":"max_output_tokens"`) || !strings.Contains(string(sent), `"role":"user"`) {
		t.Errorf("incomplete: %s, sent %s", rec.Body, sent)
	}

	for _, c := range []struct {
		name, body string
		status     int
		param      string
	}{
		{"previous response", `{"model":"qwen2.5-7b-q4","input":"hi","previous_response_id":"resp_1"}`, http.StatusBadRequest, "previous_response_id"},
		{"no input", `{"model":"qwen2.5-7b-q4"}`, http.StatusBadRequest, "input"},
		{"unknown item", `{"model":"qwen2.5-7b-q4","input":[{"type":"web_search_call"}]}`, http.StatusBadRequest, "input"},
	} {
		rec := call(c.body)
		if rec.Code != c.status || !strings.Contains(rec.Body.String(), `"param":"`+c.param+`"`) {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body)
		}
	}
	perms = auth.APIKeyPermissions{}
	if rec := call(`{"model":"qwen2.5-7b-q4","input":"hi"}`); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "RAW_NOT_ALLOWED") {
		t.Errorf("no raw access: %d %s", rec.Code, rec.Body)
	}
}
