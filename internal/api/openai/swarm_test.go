package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// swarmHandler is a handler whose raw: requests and Spread both reach the
// fake model, so what each sends can be compared.
func swarmHandler(t *testing.T, model string) (*Handler, *[]string, *auth.APIKeyPermissions) {
	t.Helper()
	h := testHandler(t)
	perms := &auth.APIKeyPermissions{Raw: true}
	h.Permissions = func(r *http.Request) (auth.APIKeyPermissions, context.Context, error) {
		return *perms, r.Context(), nil
	}
	h.Raw = func(context.Context, string, int) (string, int, func(), error) { return model, 32768, func() {}, nil }
	var spread []string
	h.Spread = func(_ context.Context, profile string, need int) (RawRoute, error) {
		if profile != "coding" {
			return RawRoute{}, ErrSwarmProfile
		}
		spread = append(spread, profile)
		return RawRoute{
			Send: func(ctx context.Context, body []byte) (*http.Response, error) {
				return SendRaw(ctx, model, body, 32768)
			},
			Done:   func() {},
			NodeID: "n2", ModelID: "m2",
		}, nil
	}
	return h, &spread, perms
}

// A swarm:<profile> request with the agent's own tools is Spread: it
// reaches the model exactly as raw: would send it, and comes back in the
// request's format, named as asked, in each of the three formats (#1135).
func TestSpreadForwardsUnchanged(t *testing.T) {
	var sent []byte
	model := fakeModel(t, &sent, chatAnswerJSON, chatStream)
	h, spread, perms := swarmHandler(t, model.URL)
	formats := []struct {
		name, body string
		serve      func(http.ResponseWriter, *http.Request)
		want       string
	}{
		{"chat completions", `{"model":"raw:qwen","messages":[{"role":"user","content":"read go.mod"}],"tools":[{"type":"function","function":{"name":"read_file","parameters":{"type":"object"}}}],"toskar":{"memory":true}}`,
			h.HandleChatCompletions, `"tool_calls"`},
		{"messages", messagesRequestJSON, h.HandleMessages, `"type":"tool_use"`},
		{"responses", responsesRequestJSON, h.HandleResponses, `"type":"function_call"`},
	}
	for _, f := range formats {
		for _, stream := range []bool{false, true} {
			body := f.body
			if stream {
				body = strings.Replace(body, "{", `{"stream":true,`, 1)
			}
			call := func(model string) (*httptest.ResponseRecorder, []byte) {
				sent = nil
				rec := httptest.NewRecorder()
				b := strings.Replace(body, `"raw:qwen2.5-7b-q4"`, `"`+model+`"`, 1)
				b = strings.Replace(b, `"raw:qwen"`, `"`+model+`"`, 1)
				f.serve(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(b)))
				return rec, sent
			}
			_, asRaw := call("raw:qwen")
			rec, asSpread := call("swarm:coding")
			// llama-server's chunks here carry no model, so only an answer
			// that has one is renamed.
			named := strings.Contains(rec.Body.String(), `"swarm:coding"`) || (stream && f.name == "chat completions")
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), f.want) || !named {
				t.Fatalf("%s (stream %v): %d %s", f.name, stream, rec.Code, rec.Body)
			}
			// The model field is the one thing that differs; llama-server
			// ignores it.
			if !bytes.Equal(withoutModel(t, asRaw), withoutModel(t, asSpread)) {
				t.Errorf("%s (stream %v): spread sent\n%s\nraw sent\n%s", f.name, stream, asSpread, asRaw)
			}
		}
	}
	if len(*spread) != 6 {
		t.Fatalf("spread %d requests", len(*spread))
	}

	// Without raw access, or pinned to a profile, a key can't spread.
	for _, p := range []auth.APIKeyPermissions{{}, {Raw: true, Profile: "coding"}} {
		*perms = p
		rec := httptest.NewRecorder()
		h.HandleChatCompletions(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(formats[0].body)))
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "RAW_NOT_ALLOWED") {
			t.Errorf("%+v: %d %s", p, rec.Code, rec.Body)
		}
	}
	*perms = auth.APIKeyPermissions{Raw: true}
	// A profile there isn't is a model there isn't.
	rec := httptest.NewRecorder()
	h.HandleChatCompletions(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Replace(formats[0].body, `"raw:qwen"`, `"swarm:nope"`, 1))))
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "MODEL_NOT_FOUND") {
		t.Errorf("unknown profile: %d %s", rec.Code, rec.Body)
	}
}

func withoutModel(t *testing.T, raw []byte) []byte {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("not JSON: %s", raw)
	}
	delete(m, "model")
	out, _ := json.Marshal(m)
	return out
}

// Client tools are a request's tools, or tool use in its conversation.
func TestClientTools(t *testing.T) {
	for body, want := range map[string]bool{
		`{"messages":[{"role":"user","content":"hi"}]}`:                                         false,
		`{"messages":[{"role":"user","content":"hi"}],"tools":[]}`:                              false,
		`{"messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function"}]}`:           true,
		`{"messages":[{"role":"assistant","tool_calls":[{"id":"1"}]},{"role":"user"}]}`:         true,
		`{"messages":[{"role":"tool","tool_call_id":"1","content":"ok"},{"role":"user"}]}`:      true,
		`{"messages":[{"role":"user","content":"hi"}],"functions":[{"name":"old_style_tool"}]}`: true,
	} {
		if got := clientTools([]byte(body)); got != want {
			t.Errorf("%s: %v", body, got)
		}
	}
}

// fakeSwarm runs a "swarm" whose progress arrives before its answer, as
// the engine's events do.
func fakeSwarm(got *struct {
	profile, prompt string
	opts            *turnopts.Options
}) func(ctx context.Context, profile, prompt string) (<-chan pluginapi.ChatChunk, string, error) {
	return func(ctx context.Context, profile, prompt string) (<-chan pluginapi.ChatChunk, string, error) {
		if profile != "coding" {
			return nil, "", ErrSwarmProfile
		}
		got.profile, got.prompt, got.opts = profile, prompt, turnopts.From(ctx)
		opts := got.opts
		ch := make(chan pluginapi.ChatChunk, 4)
		go func() {
			defer close(ch)
			opts.Progress("swarm.started", map[string]any{"mode": "split", "conversation_id": "c1", "agents": []map[string]any{
				{"agent": 1, "part": "Prices", "model": "m1"}, {"agent": 2, "part": "Reviews", "model": "m2", "status": "queued"},
			}})
			opts.Progress("swarm.agent.finished", map[string]any{"agent": 1, "status": "done", "summary": "Cheaper."})
			opts.Progress("chat.model_routed", map[string]any{"model_id": "m1"})
			ch <- pluginapi.ChatChunk{Content: "Merged "}
			ch <- pluginapi.ChatChunk{Content: "answer.", Done: true, Metrics: &pluginapi.GenerationMetrics{PromptTokens: 9, CompletionTokens: 3}}
		}()
		return ch, "c1", nil
	}
}

// A swarm:<profile> request without tools runs a full swarm on its last
// user message and answers in its format, the swarm's progress as
// reasoning where the format has it (#1135).
func TestFullSwarmAnswers(t *testing.T) {
	h := testHandler(t)
	perms := auth.APIKeyPermissions{}
	h.Permissions = func(r *http.Request) (auth.APIKeyPermissions, context.Context, error) { return perms, r.Context(), nil }
	var got struct {
		profile, prompt string
		opts            *turnopts.Options
	}
	h.Swarm = fakeSwarm(&got)
	h.Spread = func(context.Context, string, int) (RawRoute, error) {
		t.Fatal("a request without tools was spread")
		return RawRoute{}, nil
	}
	chat := `{"model":"swarm:coding","messages":[{"role":"system","content":"Be brief."},{"role":"user","content":"Earlier question"},{"role":"assistant","content":"Earlier answer"},{"role":"user","content":"Compare the two"}]}`
	messages := `{"model":"swarm:coding","max_tokens":1024,"system":"Be brief.","messages":[{"role":"user","content":"Earlier question"},{"role":"assistant","content":"Earlier answer"},{"role":"user","content":[{"type":"text","text":"Compare the two"}]}]}`
	responses := `{"model":"swarm:coding","instructions":"Be brief.","input":[{"role":"user","content":"Earlier question"},{"role":"assistant","content":[{"type":"output_text","text":"Earlier answer"}]},{"role":"user","content":[{"type":"input_text","text":"Compare the two"}]}]}`
	for _, c := range []struct {
		name, body string
		serve      func(http.ResponseWriter, *http.Request)
		answer     []string
		stream     []string
	}{
		{"chat completions", chat, h.HandleChatCompletions,
			[]string{`"content":"Merged answer."`, `"reasoning_content":"Swarm (split) started with 2 agents, in the chat /chat?c=c1.`, `"conversation_id":"c1"`, `"model":"swarm:coding"`},
			[]string{`"reasoning_content":"Agent 1 finished: Cheaper.\n"`, `"content":"Merged "`, `data: [DONE]`}},
		{"messages", messages, h.HandleMessages,
			[]string{`{"signature":"","thinking":"Swarm (split)`, `Agent 2: Reviews (m2), waiting for a free slot.`, `{"text":"Merged answer.","type":"text"}`, `"stop_reason":"end_turn"`, `"output_tokens":3`},
			[]string{`"type":"thinking_delta"`, `"text":"Merged ","type":"text_delta"`, `event: message_stop`}},
		{"responses", responses, h.HandleResponses,
			[]string{`"type":"reasoning"`, `"type":"summary_text"`, `"text":"Merged answer."`, `"status":"completed"`},
			[]string{`event: response.reasoning_summary_text.delta`, `event: response.output_text.delta`, `event: response.completed`}},
	} {
		for _, stream := range []bool{false, true} {
			body := c.body
			want := c.answer
			if stream {
				body = strings.Replace(body, "{", `{"stream":true,`, 1)
				want = c.stream
			}
			rec := httptest.NewRecorder()
			c.serve(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
			out, _ := io.ReadAll(rec.Body)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s (stream %v): %d %s", c.name, stream, rec.Code, out)
			}
			for _, w := range want {
				if !strings.Contains(string(out), w) {
					t.Errorf("%s (stream %v): no %s in\n%s", c.name, stream, w, out)
				}
			}
			if got.prompt != "Compare the two" || got.opts == nil || got.opts.System != "Be brief." || len(got.opts.History) != 2 {
				t.Errorf("%s: ran %q with %+v", c.name, got.prompt, got.opts)
			}
		}
	}

	call := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.HandleChatCompletions(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
		return rec
	}
	// It follows profile:'s rules: a key pinned to another profile can't,
	// and a chat-only key saves no chats, which a swarm keeps its agents in.
	for _, c := range []struct {
		perms auth.APIKeyPermissions
		code  string
	}{
		{auth.APIKeyPermissions{Profile: "general"}, "PROFILE_PINNED"},
		{auth.APIKeyPermissions{Routes: auth.RoutesChat}, "KEY_CHAT_ONLY"},
	} {
		perms = c.perms
		if rec := call(chat); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), c.code) {
			t.Errorf("%+v: %d %s", c.perms, rec.Code, rec.Body)
		}
	}
	perms = auth.APIKeyPermissions{Profile: "coding"}
	if rec := call(chat); rec.Code != http.StatusOK {
		t.Errorf("pinned to its own profile: %d %s", rec.Code, rec.Body)
	}
	perms = auth.APIKeyPermissions{}
	if rec := call(strings.Replace(chat, "swarm:coding", "swarm:nope", 1)); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "MODEL_NOT_FOUND") {
		t.Errorf("unknown profile: %d %s", rec.Code, rec.Body)
	}
	h.Swarm = func(context.Context, string, string) (<-chan pluginapi.ChatChunk, string, error) {
		return nil, "", contracts.NewError("ROLE_REQUIRED", nil, io.EOF)
	}
	if rec := call(chat); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "ROLE_REQUIRED") {
		t.Errorf("a visitor: %d %s", rec.Code, rec.Body)
	}
}

// /v1/models lists swarm:<profile> for profiles whose strategy is swarm.
func TestSwarmModelsListed(t *testing.T) {
	h := testHandler(t)
	if _, err := h.Profiles.Create(context.Background(), profiles.Profile{
		ID: "coding", Name: "Coding", OrchestratorID: "simple",
		Orchestration: contracts.OrchestrationPolicy{Strategy: profiles.StrategySwarm},
	}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		perms auth.APIKeyPermissions
		want  []string
		not   []string
	}{
		{auth.APIKeyPermissions{}, []string{`"swarm:coding"`, `"profile:general"`}, []string{`"swarm:general"`}},
		{auth.APIKeyPermissions{Profile: "coding"}, []string{`"swarm:coding"`, `"profile:coding"`}, []string{`"profile:general"`}},
		{auth.APIKeyPermissions{Profile: "general"}, []string{`"profile:general"`}, []string{`"swarm:`}},
	} {
		h.Permissions = func(r *http.Request) (auth.APIKeyPermissions, context.Context, error) {
			return c.perms, r.Context(), nil
		}
		rec := httptest.NewRecorder()
		h.HandleModels(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
		for _, w := range c.want {
			if !strings.Contains(rec.Body.String(), w) {
				t.Errorf("%+v: no %s in %s", c.perms, w, rec.Body)
			}
		}
		for _, n := range c.not {
			if strings.Contains(rec.Body.String(), n) {
				t.Errorf("%+v: %s in %s", c.perms, n, rec.Body)
			}
		}
	}
}
