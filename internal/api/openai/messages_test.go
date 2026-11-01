package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/auth"
)

// goldenFile compares got with testdata/<name>, its ids masked, as golden
// does for Chat Completions; UPDATE_OPENAI_GOLDEN=1 writes it (#843).
func goldenFile(t *testing.T, name string, got []byte) {
	t.Helper()
	got = regexp.MustCompile(`(msg|toolu)_[0-9a-f]{24}`).ReplaceAll(got, []byte("${1}_ID"))
	path := filepath.Join("testdata", name)
	if os.Getenv("UPDATE_OPENAI_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (UPDATE_OPENAI_GOLDEN=1 writes it)", name, err)
	}
	if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
		t.Errorf("%s differs:\n got  %s\n want %s", name, got, want)
	}
}

// indent is JSON, indented, so a golden file diffs by line.
func indent(t *testing.T, raw []byte) []byte {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("not JSON: %s", raw)
	}
	out, _ := json.MarshalIndent(v, "", "  ")
	return append(out, '\n')
}

// fakeModel is a llama-server that keeps what it was sent and answers with
// one of the canned Chat Completions answers.
func fakeModel(t *testing.T, sent *[]byte, answer, stream string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*sent, _ = io.ReadAll(r.Body)
		var req struct {
			Stream bool `json:"stream"`
		}
		_ = json.Unmarshal(*sent, &req)
		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, stream)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, answer)
	}))
	t.Cleanup(s.Close)
	return s
}

// An agent's turn in Anthropic's format: a system prompt, an earlier tool
// call and its result, a picture, the agent's tools, and a forced tool.
const messagesRequestJSON = `{
  "model": "raw:qwen2.5-7b-q4",
  "max_tokens": 512,
  "system": [{"type": "text", "text": "You are a coding agent."}, {"type": "text", "text": "Use the tools."}],
  "messages": [
    {"role": "user", "content": "What's in main.go?"},
    {"role": "assistant", "content": [
      {"type": "text", "text": "Let me look."},
      {"type": "tool_use", "id": "toolu_01", "name": "read_file", "input": {"path": "main.go"}}
    ]},
    {"role": "user", "content": [
      {"type": "tool_result", "tool_use_id": "toolu_01", "content": [{"type": "text", "text": "package main"}]},
      {"type": "text", "text": "And this screenshot?"},
      {"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "iVBORw0KGgo="}}
    ]}
  ],
  "tools": [{"name": "read_file", "description": "Read a file", "input_schema": {"type": "object", "properties": {"path": {"type": "string"}}, "required": ["path"]}}],
  "tool_choice": {"type": "any"},
  "stop_sequences": ["</done>"],
  "temperature": 0.2,
  "toskar": {"context_window": 16384}
}`

// The model's answer: a sentence and a tool call, as llama-server gives it.
const chatAnswerJSON = `{"id":"chatcmpl-1","object":"chat.completion","model":"gpt-3.5-turbo","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":"I'll read it.","tool_calls":[{"id":"call_7","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"go.mod\"}"}}]}}],"usage":{"prompt_tokens":120,"completion_tokens":18,"total_tokens":138}}`

const chatStream = "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":null}}]}\n\n" +
	"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"I'll \"}}]}\n\n" +
	"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"read it.\"}}]}\n\n" +
	"data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_7\",\"type\":\"function\",\"function\":{\"name\":\"read_file\",\"arguments\":\"{\\\"pa\"}}]}}]}\n\n" +
	"data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"th\\\":\\\"go.mod\\\"}\"}}]}}]}\n\n" +
	"data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
	"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":120,\"completion_tokens\":18}}\n\n" +
	"data: [DONE]\n\n"

// Anthropic's format reaches the model as Chat Completions, and the answer
// comes back as Anthropic's, whole or as its events (#843).
func TestMessages(t *testing.T) {
	var sent []byte
	model := fakeModel(t, &sent, chatAnswerJSON, chatStream)
	h := testHandler(t)
	perms := auth.APIKeyPermissions{Raw: true}
	h.Permissions = func(r *http.Request) (auth.APIKeyPermissions, context.Context, error) { return perms, r.Context(), nil }
	var need int
	h.Raw = func(_ context.Context, id string, n int) (string, int, func(), error) {
		need = n
		if id != "qwen2.5-7b-q4" {
			return "", 0, nil, ErrRawNotHere
		}
		return model.URL, 32768, func() {}, nil
	}
	call := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.HandleMessages(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body)))
		return rec
	}

	rec := call(messagesRequestJSON)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	goldenFile(t, "messages/request.chat.json", indent(t, sent))
	goldenFile(t, "messages/answer.json", indent(t, rec.Body.Bytes()))
	if need < 16384 {
		t.Errorf("asked for a window of %d, under toskar.context_window", need)
	}

	streamed := strings.Replace(messagesRequestJSON, `"max_tokens": 512,`, `"max_tokens": 512, "stream": true,`, 1)
	rec = call(streamed)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream: %d %s", rec.Code, rec.Body)
	}
	goldenFile(t, "messages/answer.stream.txt", rec.Body.Bytes())

	for _, c := range []struct {
		name, body string
		status     int
		kind       string
	}{
		{"no max_tokens", `{"model":"raw:qwen2.5-7b-q4","messages":[{"role":"user","content":"hi"}]}`, http.StatusBadRequest, "invalid_request_error"},
		{"not here", `{"model":"other","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`, http.StatusNotFound, "not_found_error"},
		{"bad role", `{"model":"qwen2.5-7b-q4","max_tokens":10,"messages":[{"role":"tool","content":"hi"}]}`, http.StatusBadRequest, "invalid_request_error"},
	} {
		rec := call(c.body)
		if rec.Code != c.status || !strings.Contains(rec.Body.String(), `"type":"`+c.kind+`"`) {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body)
		}
	}
	perms = auth.APIKeyPermissions{}
	if rec := call(`{"model":"qwen2.5-7b-q4","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "permission_error") {
		t.Errorf("no raw access: %d %s", rec.Code, rec.Body)
	}
}

// count_tokens estimates a request's input, as agents ask before sending.
func TestCountTokens(t *testing.T) {
	h := testHandler(t)
	rec := httptest.NewRecorder()
	h.HandleCountTokens(rec, httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(messagesRequestJSON)))
	var got struct {
		InputTokens int `json:"input_tokens"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != http.StatusOK || got.InputTokens < 20 {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
}

// Claude Code sends a system turn partway, whose tool_addition blocks add
// tools, and asks for 32,000 max_tokens as a ceiling (#959): the turn's
// text becomes instructions, its tools join the request's, and the window
// is sized for the prompt and a bounded answer, not 32,000 more.
func TestMessagesFromClaudeCode(t *testing.T) {
	var sent []byte
	model := fakeModel(t, &sent, chatAnswerJSON, chatStream)
	h := testHandler(t)
	h.Permissions = func(r *http.Request) (auth.APIKeyPermissions, context.Context, error) {
		return auth.APIKeyPermissions{Raw: true}, r.Context(), nil
	}
	var need int
	h.Raw = func(_ context.Context, _ string, n int) (string, int, func(), error) {
		need = n
		return model.URL, 32768, func() {}, nil
	}
	body := `{"model":"qwen2.5-7b-q4","max_tokens":32000,"thinking":{"type":"adaptive"},"context_management":{"edits":[]},"output_config":{"effort":"high"},
	  "system":[{"type":"text","text":"You are Claude Code."}],
	  "tools":[{"name":"Read","description":"Read a file","input_schema":{"type":"object"}}],
	  "messages":[
	    {"role":"user","content":[{"type":"text","text":"What's in main.go?"}]},
	    {"role":"system","content":[{"type":"text","text":"# Environment"},{"type":"tool_addition","tool":{"type":"tool_definition","definition":{"name":"Grep","description":"Search files","input_schema":{"type":"object"}}}}]}
	  ]}`
	rec := httptest.NewRecorder()
	h.HandleMessages(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var got struct {
		Messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(sent, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 3 || got.Messages[2].Role != "system" || got.Messages[2].Content != "# Environment" {
		t.Errorf("messages: %+v", got.Messages)
	}
	if len(got.Tools) != 2 || got.Tools[1].Function.Name != "Grep" {
		t.Errorf("tools: %+v", got.Tools)
	}
	if need > rawAnswerRoom+1000 {
		t.Errorf("sized a window of %d for a short prompt", need)
	}
}
