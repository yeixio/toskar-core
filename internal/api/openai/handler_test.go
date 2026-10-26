package openai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

func TestHandleModelsRequiresAuthAndListsProfiles(t *testing.T) {
	h := testHandler(t)
	h.Auth = func(*http.Request) error { return errors.New("missing key") }
	rec := httptest.NewRecorder()
	h.HandleModels(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	h.Auth = nil
	rec = httptest.NewRecorder()
	h.HandleModels(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"id":"profile:general"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleChatCompletions(t *testing.T) {
	h := testHandler(t)
	chat := &scriptChat{chunks: []pluginapi.ChatChunk{{Content: "hello "}, {Content: "there", Done: true}}}
	h.Chat = chat
	h.Bus = events.NewBus(4)
	_, eventsCh := h.Bus.Subscribe()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"profile:general","messages":[{"role":"system","content":"be brief"},{"role":"user","content":"hi"}]}`))
	h.HandleChatCompletions(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "hello there") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if chat.profileID != "general" || chat.message != "hi" || chat.modelID != "" {
		t.Fatalf("chat=%+v", chat)
	}

	chat.chunks = []pluginapi.ChatChunk{{Content: "streamed", Done: true}}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"missing-profile","messages":[{"role":"user","content":"go"}],"stream":true}`))
	h.HandleChatCompletions(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "streamed") || !strings.Contains(body, "[DONE]") {
		t.Fatalf("status=%d body=%s", rec.Code, body)
	}
	if chat.profileID != "" || chat.modelID != "missing-profile" || !chat.stream {
		t.Fatalf("chat=%+v", chat)
	}
	if !sawEvent(eventsCh, events.ChatToken) || !sawEvent(eventsCh, events.ChatComplete) {
		t.Fatal("stream did not publish chat events")
	}
}

func TestHandleChatCompletionsRejectsBadRequests(t *testing.T) {
	h := testHandler(t)
	h.Chat = &scriptChat{}

	cases := []struct {
		body string
		code int
	}{
		{body: "{", code: http.StatusBadRequest},
		{body: `{"model":"general","messages":[{"role":"assistant","content":"no user"}]}`, code: http.StatusBadRequest},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		h.HandleChatCompletions(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(tc.body)))
		if rec.Code != tc.code {
			t.Fatalf("body %s status=%d", tc.body, rec.Code)
		}
	}

	h.Chat = nil
	rec := httptest.NewRecorder()
	h.HandleChatCompletions(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"general","messages":[{"role":"user","content":"hi"}]}`)))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d", rec.Code)
	}

	h.Chat = &scriptChat{err: errors.New("down")}
	rec = httptest.NewRecorder()
	h.HandleChatCompletions(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"general","messages":[{"role":"user","content":"hi"}]}`)))
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "down") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	h.Chat = &scriptChat{chunks: []pluginapi.ChatChunk{{Error: "model failed"}}}
	rec = httptest.NewRecorder()
	h.HandleChatCompletions(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"general","messages":[{"role":"user","content":"hi"}]}`)))
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "model failed") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDrainDiscardsTheBody(t *testing.T) {
	Drain(strings.NewReader("unused"))
	Drain(io.LimitReader(strings.NewReader("unused"), 2))
}

func testHandler(t *testing.T) *Handler {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	mgr := profiles.NewManager(db.SQL)
	if _, err := mgr.Create(context.Background(), profiles.Profile{
		ID:             "general",
		Name:           "General",
		Purpose:        "chat",
		OrchestratorID: "simple",
		Roles:          []contracts.ModelRole{{Role: "assistant", ModelID: "m1", Required: true}},
	}); err != nil {
		t.Fatal(err)
	}
	return &Handler{Profiles: mgr}
}

type scriptChat struct {
	chunks    []pluginapi.ChatChunk
	err       error
	profileID string
	modelID   string
	message   string
	stream    bool
	execution string
	effort    huginn.Effort
	opts      *turnopts.Options
	sampling  pluginapi.GenerateOptions
	// emit runs during the turn, as the orchestrator would.
	emit func(o *turnopts.Options)
}

func (s *scriptChat) RunChat(ctx context.Context, profileID, conversationID, message string, stream bool, modelID, execution string) (<-chan pluginapi.ChatChunk, error) {
	s.profileID = profileID
	s.modelID = modelID
	s.message = message
	s.stream = stream
	s.execution = execution
	s.effort = huginn.EffortFrom(ctx)
	s.opts = turnopts.From(ctx)
	s.sampling = pluginapi.AnswerOptionsFrom(ctx)
	if s.err != nil {
		return nil, s.err
	}
	if s.emit != nil && s.opts != nil {
		s.emit(s.opts)
	}
	ch := make(chan pluginapi.ChatChunk, len(s.chunks))
	for _, chunk := range s.chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

func sawEvent(ch <-chan events.Event, eventType string) bool {
	for {
		select {
		case event := <-ch:
			if event.Type == eventType {
				return true
			}
		default:
			return false
		}
	}
}

// temperature and max_tokens reach the turn as the answer's options (#70);
// unset, the model's defaults stand; 0 is greedy, and out of range is refused.
func TestChatCompletionsSampling(t *testing.T) {
	h := testHandler(t)
	chat := &scriptChat{chunks: []pluginapi.ChatChunk{{Content: "ok", Done: true}}}
	h.Chat = chat
	call := func(extra string) int {
		rec := httptest.NewRecorder()
		body := `{"model":"profile:general","messages":[{"role":"user","content":"hi"}]` + extra + `}`
		h.HandleChatCompletions(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
		return rec.Code
	}
	for extra, want := range map[string]pluginapi.GenerateOptions{
		``:                                   {},
		`,"temperature":0.7,"max_tokens":64`: {Temperature: 0.7, MaxTokens: 64},
		`,"temperature":0`:                   {Temperature: pluginapi.GreedyTemperature},
		`,"max_completion_tokens":32`:        {MaxTokens: 32},
	} {
		chat.sampling = pluginapi.GenerateOptions{Temperature: 99}
		if code := call(extra); code != http.StatusOK || chat.sampling != want {
			t.Errorf("%q: %d, options %+v, want %+v", extra, code, chat.sampling, want)
		}
	}
	for _, bad := range []string{`,"temperature":2.5`, `,"temperature":-1`, `,"max_tokens":-5`} {
		if code := call(bad); code != http.StatusBadRequest {
			t.Errorf("%q: %d, want 400", bad, code)
		}
	}
}

// A reasoning model's thinking comes back as reasoning_content only when
// the request asks for it with include_reasoning (#701).
func TestIncludeReasoning(t *testing.T) {
	h := testHandler(t)
	chunks := func() []pluginapi.ChatChunk {
		return []pluginapi.ChatChunk{{Reasoning: "17 × 23 is 391."}, {Content: "391", Done: true}}
	}
	ask := func(body string) string {
		h.Chat = &scriptChat{chunks: chunks()}
		rec := httptest.NewRecorder()
		h.HandleChatCompletions(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}
	const msgs = `"model":"profile:general","messages":[{"role":"user","content":"17*23?"}]`
	if body := ask(`{` + msgs + `}`); strings.Contains(body, "reasoning_content") || !strings.Contains(body, "391") {
		t.Fatalf("without include_reasoning: %s", body)
	}
	if body := ask(`{` + msgs + `,"include_reasoning":true}`); !strings.Contains(body, `"reasoning_content":"17 × 23 is 391."`) {
		t.Fatalf("with include_reasoning: %s", body)
	}
	if body := ask(`{` + msgs + `,"include_reasoning":true,"stream":true}`); !strings.Contains(body, `"delta":{"reasoning_content":"17 × 23 is 391."}`) || !strings.Contains(body, `"content":"391"`) {
		t.Fatalf("streamed with include_reasoning: %s", body)
	}
	if body := ask(`{` + msgs + `,"stream":true}`); strings.Contains(body, "reasoning_content") {
		t.Fatalf("streamed without include_reasoning: %s", body)
	}
}

// A request that names a model means that model: another doesn't answer in
// its place when it fails, unless the request allows it (#871). auto and
// profiles still fall back.
func TestNamedModelAnswersOrFails(t *testing.T) {
	h := testHandler(t)
	chat := &scriptChat{chunks: []pluginapi.ChatChunk{{Content: "ok", Done: true}}}
	h.Chat = chat
	for model, want := range map[string]bool{
		`"qwen2.5-14b-q4"`:  true,
		`"sai:support"`:     true,
		`"auto"`:            false,
		`"profile:general"`: false,
		`"general"`:         false,
		`"qwen2.5-14b-q4","toskar":{"fallback":true}`: false,
	} {
		rec := httptest.NewRecorder()
		body := `{"model":` + model + `,"messages":[{"role":"user","content":"hi"}]}`
		h.HandleChatCompletions(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
		if rec.Code != http.StatusOK || chat.opts == nil {
			t.Fatalf("%s: %d %s", model, rec.Code, rec.Body)
		}
		if chat.opts.ExactModel != want {
			t.Errorf("%s: exact model = %v, want %v", model, chat.opts.ExactModel, want)
		}
	}
}
