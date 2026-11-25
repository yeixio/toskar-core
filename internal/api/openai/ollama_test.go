package openai

import (
	"bufio"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Ollama's API (#1356): an app that offers only Ollama lists Toskar's
// models, chats through the same path as Chat Completions, streamed a JSON
// object per line, and is told plainly that models are managed in Toskar.
func TestOllamaChat(t *testing.T) {
	h := testHandler(t)
	chat := &scriptChat{chunks: []pluginapi.ChatChunk{{Content: "hello "}, {Content: "there", Done: true}}}
	h.Chat = chat

	// Not streamed: one object, with ":latest" dropped from the name.
	rec := httptest.NewRecorder()
	h.HandleOllamaChat(rec, httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(
		`{"model":"profile:general:latest","stream":false,"messages":[{"role":"system","content":"be brief"},{"role":"user","content":"hi"}],"options":{"temperature":0.3}}`)))
	var answer struct {
		Model   string `json:"model"`
		Done    bool   `json:"done"`
		Message struct {
			Role, Content string
		} `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !answer.Done || answer.Message.Content != "hello there" || answer.Message.Role != "assistant" {
		t.Fatalf("answer = %+v", answer)
	}
	if chat.profileID != "general" || chat.message != "hi" || chat.opts == nil || chat.opts.System != "be brief" {
		t.Fatalf("chat = %+v", chat)
	}
	if chat.sampling.Temperature != 0.3 {
		t.Fatalf("temperature = %+v", chat.sampling)
	}

	// Streamed by default: a line per piece, then done.
	chat.chunks = []pluginapi.ChatChunk{{Content: "one "}, {Content: "two", Done: true}}
	rec = httptest.NewRecorder()
	h.HandleOllamaChat(rec, httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{"model":"auto","messages":[{"role":"user","content":"count"}]}`)))
	if ct := rec.Header().Get("Content-Type"); ct != "application/x-ndjson" {
		t.Fatalf("content type %q, body %s", ct, rec.Body.String())
	}
	var text strings.Builder
	var last map[string]any
	sc := bufio.NewScanner(rec.Body)
	for sc.Scan() {
		var line map[string]any
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			t.Fatalf("line %q: %v", sc.Text(), err)
		}
		if msg, ok := line["message"].(map[string]any); ok {
			text.WriteString(msg["content"].(string))
		}
		last = line
	}
	if text.String() != "one two" || last["done"] != true || last["done_reason"] != "stop" || !chat.stream {
		t.Fatalf("streamed %q, last %v", text.String(), last)
	}
}

func TestOllamaGenerateAndErrors(t *testing.T) {
	h := testHandler(t)
	chat := &scriptChat{chunks: []pluginapi.ChatChunk{{Content: "a poem", Done: true}}}
	h.Chat = chat
	rec := httptest.NewRecorder()
	h.HandleOllamaGenerate(rec, httptest.NewRequest(http.MethodPost, "/api/generate", strings.NewReader(`{"model":"auto","prompt":"write","system":"rhyme","stream":false}`)))
	if !strings.Contains(rec.Body.String(), `"response":"a poem"`) || chat.message != "write" || chat.opts.System != "rhyme" {
		t.Fatalf("status=%d body=%s chat=%+v", rec.Code, rec.Body.String(), chat)
	}

	// An empty prompt loads the model, as in Ollama.
	rec = httptest.NewRecorder()
	h.HandleOllamaGenerate(rec, httptest.NewRequest(http.MethodPost, "/api/generate", strings.NewReader(`{"model":"auto","stream":false}`)))
	if !strings.Contains(rec.Body.String(), `"done_reason":"load"`) {
		t.Fatalf("load: %s", rec.Body.String())
	}

	// A failure is Ollama's {"error"}, streamed or not.
	h.Chat = &scriptChat{err: errors.New("down")}
	for _, stream := range []string{"true", "false"} {
		rec = httptest.NewRecorder()
		h.HandleOllamaChat(rec, httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{"model":"auto","stream":`+stream+`,"messages":[{"role":"user","content":"hi"}]}`)))
		// Streamed, it may come after the answer began, as Ollama sends it.
		if !strings.Contains(rec.Body.String(), `"error":"`) || !strings.Contains(rec.Body.String(), "down") || strings.Contains(rec.Body.String(), `"done":true`) {
			t.Fatalf("stream %s: status=%d body=%s", stream, rec.Code, rec.Body.String())
		}
	}

	rec = httptest.NewRecorder()
	h.HandleOllamaManage(rec, httptest.NewRequest(http.MethodPost, "/api/pull", strings.NewReader(`{"model":"llama3"}`)))
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "Models page") {
		t.Fatalf("pull: %d %s", rec.Code, rec.Body.String())
	}
}

func TestOllamaModels(t *testing.T) {
	h := testHandler(t)
	rec := httptest.NewRecorder()
	h.HandleOllamaTags(rec, httptest.NewRequest(http.MethodGet, "/api/tags", nil))
	var tags struct {
		Models []struct{ Name, Model string } `json:"models"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tags); err != nil || len(tags.Models) < 2 || tags.Models[0].Name != "auto" || tags.Models[1].Name != "profile:general" {
		t.Fatalf("tags: %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.HandleOllamaShow(rec, httptest.NewRequest(http.MethodPost, "/api/show", strings.NewReader(`{"model":"profile:general:latest"}`)))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"capabilities":["completion","vision"]`) {
		t.Fatalf("show: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.HandleOllamaShow(rec, httptest.NewRequest(http.MethodPost, "/api/show", strings.NewReader(`{"model":"nothing"}`)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown model: %d", rec.Code)
	}

	// A key that can't list models can't use these either.
	h.Auth = func(*http.Request) error { return errors.New("missing key") }
	rec = httptest.NewRecorder()
	h.HandleOllamaTags(rec, httptest.NewRequest(http.MethodGet, "/api/tags", nil))
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), `"error":"missing key"`) {
		t.Fatalf("unauthorized: %d %s", rec.Code, rec.Body.String())
	}
}

func TestOllamaEmbed(t *testing.T) {
	h := testHandler(t)
	h.Embed = func(_ *http.Request, texts []string) ([][]float32, string, error) {
		out := make([][]float32, len(texts))
		for i := range texts {
			out[i] = []float32{float32(i), 1}
		}
		return out, "nomic", nil
	}
	rec := httptest.NewRecorder()
	h.HandleOllamaEmbed(rec, httptest.NewRequest(http.MethodPost, "/api/embed", strings.NewReader(`{"model":"x","input":["a","b"]}`)))
	if rec.Body.String() != `{"embeddings":[[0,1],[1,1]],"model":"nomic"}`+"\n" {
		t.Fatalf("embed: %s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.HandleOllamaEmbed(rec, httptest.NewRequest(http.MethodPost, "/api/embeddings", strings.NewReader(`{"model":"x","prompt":"a"}`)))
	if rec.Body.String() != `{"embedding":[0,1]}`+"\n" {
		t.Fatalf("legacy embed: %s", rec.Body.String())
	}
}

// Ollama's images and tool calls reach Chat Completions' shape.
func TestOllamaMessageShape(t *testing.T) {
	m := ollamaMessage{Role: "assistant", ToolCalls: []ollamaToolCall{{}}}
	m.ToolCalls[0].Function.Name = "turn_on"
	m.ToolCalls[0].Function.Arguments = json.RawMessage(`{"entity":"light.kitchen"}`)
	got, _ := json.Marshal(m.chatMessage(2))
	if !strings.Contains(string(got), `"arguments":"{\"entity\":\"light.kitchen\"}"`) || !strings.Contains(string(got), `"id":"call_2_0"`) {
		t.Fatalf("tool call: %s", got)
	}
	pic, _ := json.Marshal(ollamaMessage{Role: "user", Content: "what's this", Images: []string{"/9j/abc"}}.chatMessage(0))
	if !strings.Contains(string(pic), `data:image/jpeg;base64,/9j/abc`) {
		t.Fatalf("image: %s", pic)
	}
	body, _ := chatBody("auto", nil, false, json.RawMessage(`"json"`), ollamaOptions{NumPredict: 50}, nil, json.RawMessage(`false`))
	if !strings.Contains(string(body), `"response_format":{"type":"json_object"}`) || !strings.Contains(string(body), `"max_tokens":50`) || !strings.Contains(string(body), `"reasoning_effort":"none"`) {
		t.Fatalf("body: %s", body)
	}
}
