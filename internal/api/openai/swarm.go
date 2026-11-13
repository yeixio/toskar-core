package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Swarms for editor agents (#1135, docs/features/swarms.md#spread-editor-agents-subagents):
// swarm:<profile> on the three raw formats.
//
//   - A request with client tools, or any tool use in its conversation, is
//     Spread: it goes to the least busy of the profile's swarm models and
//     computers that fits, as it came, and the answer comes back in the
//     request's format, as raw:<model> does. An editor agent's subagents
//     send such requests at once, and they run at once instead of queueing
//     on one model.
//   - A request without them runs a full swarm on its last user message,
//     and answers with the coordinator's answer. Its progress streams as
//     reasoning: reasoning_content, a thinking block, or a reasoning item.

// SwarmPrefix names a profile's swarm: swarm:<profile id>.
const SwarmPrefix = "swarm:"

// ErrSwarmProfile is a swarm:<profile> that names no profile.
var ErrSwarmProfile = errors.New("there's no profile with that id")

// spreadReady readies a Spread request: the key may drive models directly,
// as raw:<model> needs, and the profile's least busy model and computer
// that fits takes it.
func (h *Handler) spreadReady(ctx context.Context, perms auth.APIKeyPermissions, profile string, need int) (RawRoute, *rawFailure) {
	if fail := rawAllowed(perms); fail != nil {
		fail.message = "this key can't drive models directly, which a swarm:<profile> request with tools does; an Admin can allow raw model access for it in API Access"
		return RawRoute{}, fail
	}
	if profile == "" {
		return RawRoute{}, &rawFailure{status: http.StatusBadRequest, code: "INVALID_REQUEST", message: "name the profile, such as swarm:coding", param: "model"}
	}
	if h.Spread == nil {
		return RawRoute{}, &rawFailure{status: http.StatusNotImplemented, code: "NOT_IMPLEMENTED", message: "swarms aren't available"}
	}
	route, err := h.Spread(ctx, profile, need)
	if err != nil {
		return RawRoute{}, h.rawError(SwarmPrefix+profile, err)
	}
	return route, nil
}

// clientTools reports a Chat Completions body that brings its own tools,
// or whose conversation used them: a request an editor agent runs the tools
// of, which Spread forwards as it came.
func clientTools(body []byte) bool {
	var req struct {
		Tools     []json.RawMessage `json:"tools"`
		Functions []json.RawMessage `json:"functions"`
		Messages  []struct {
			Role      string            `json:"role"`
			ToolCalls []json.RawMessage `json:"tool_calls"`
		} `json:"messages"`
	}
	if json.Unmarshal(body, &req) != nil {
		return false
	}
	if len(req.Tools) > 0 || len(req.Functions) > 0 {
		return true
	}
	for _, m := range req.Messages {
		if m.Role == "tool" || m.Role == "function" || len(m.ToolCalls) > 0 {
			return true
		}
	}
	return false
}

// chatTurns are a Chat Completions body's messages as text, for a full
// swarm: its last user message is the prompt.
func chatTurns(body []byte) []pluginapi.ChatMessage {
	var req struct {
		Messages []map[string]any `json:"messages"`
	}
	_ = json.Unmarshal(body, &req)
	out := make([]pluginapi.ChatMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		role, _ := m["role"].(string)
		out = append(out, pluginapi.ChatMessage{Role: role, Content: messageText(m["content"])})
	}
	return out
}

// swarmFeed is a full swarm's answer and its progress, as one stream of
// chunks: progress arrives from the swarm's agents as they go, as
// reasoning.
type swarmFeed struct {
	ctx    context.Context
	mu     sync.Mutex
	out    chan pluginapi.ChatChunk
	closed bool
}

func (f *swarmFeed) send(c pluginapi.ChatChunk) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	select {
	case f.out <- c:
	case <-f.ctx.Done():
	}
}

func (f *swarmFeed) close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		close(f.out)
	}
}

// swarmRun starts a full swarm for a swarm:<profile> request without
// client tools, as profile:<id> runs the assistant: the key's permissions
// apply, a key pinned to a profile may only name its own, and starting a
// swarm needs the member role. It returns the answer with its progress as
// reasoning, and the coordinator chat's id.
func (h *Handler) swarmRun(r *http.Request, perms auth.APIKeyPermissions, req chatCompletionRequest, sampling pluginapi.GenerateOptions) (<-chan pluginapi.ChatChunk, string, *rawFailure) {
	profile := strings.TrimPrefix(req.Model, SwarmPrefix)
	switch {
	case profile == "":
		return nil, "", &rawFailure{status: http.StatusBadRequest, code: "INVALID_REQUEST", message: "name the profile, such as swarm:coding", param: "model"}
	case h.Swarm == nil:
		return nil, "", &rawFailure{status: http.StatusNotImplemented, code: "NOT_IMPLEMENTED", message: "swarms aren't available"}
	case perms.Routes == auth.RoutesChat:
		// A swarm keeps its agents in chats, and a chat-only key saves none.
		return nil, "", &rawFailure{status: http.StatusForbidden, code: "KEY_CHAT_ONLY", message: "this key is for chat only, and a swarm keeps its agents in chats; send tools to spread the request instead, or use another key"}
	case perms.Profile != "" && perms.Profile != profile:
		return nil, "", &rawFailure{status: http.StatusForbidden, code: "PROFILE_PINNED", message: "this key answers only with profile:" + perms.Profile + `; use model "swarm:` + perms.Profile + `"`, param: "model"}
	}
	message, history, system := splitMessages(req.Messages)
	if message == "" {
		return nil, "", &rawFailure{status: http.StatusBadRequest, code: "INVALID_REQUEST", message: "messages must include a user message", param: "messages"}
	}
	opts, _, effort, err := turnOptions(req, perms)
	if err != nil {
		return nil, "", &rawFailure{status: http.StatusForbidden, code: "NOT_ALLOWED", message: err.Error()}
	}
	opts.History, opts.System = history, system
	feed := &swarmFeed{ctx: r.Context(), out: make(chan pluginapi.ChatChunk, 16)}
	opts.Progress = func(eventType string, payload map[string]any) {
		if line := swarmLine(eventType, payload); line != "" {
			feed.send(pluginapi.ChatChunk{Reasoning: line + "\n"})
		}
	}
	ctx := turnopts.With(r.Context(), opts)
	if sampling != (pluginapi.GenerateOptions{}) {
		ctx = pluginapi.WithAnswerOptions(ctx, sampling)
	}
	if effort != "" {
		ctx = huginn.WithEffort(ctx, huginn.ParseEffort(effort))
	}
	stream, conversationID, err := h.Swarm(ctx, profile, message)
	if err != nil {
		feed.close()
		if errors.Is(err, ErrSwarmProfile) {
			return nil, "", &rawFailure{status: http.StatusNotFound, code: "MODEL_NOT_FOUND", message: req.Model + ": " + err.Error(), param: "model"}
		}
		if code, _ := contracts.ErrorCode(err); code == "ROLE_REQUIRED" || code == "PORTAL_MEMBERS" {
			return nil, "", &rawFailure{status: http.StatusForbidden, code: code, message: err.Error()}
		}
		status, code := h.failure("", err.Error())
		return nil, "", &rawFailure{status: status, code: code, message: err.Error()}
	}
	go func() {
		defer feed.close()
		for c := range stream {
			feed.send(c)
		}
	}()
	return feed.out, conversationID, nil
}

// swarmLine is a swarm event as a line of progress, or "" for one that
// says nothing new.
func swarmLine(eventType string, p map[string]any) string {
	str := func(k string) string {
		s, _ := p[k].(string)
		return strings.TrimSpace(s)
	}
	num := func(m map[string]any, k string) int {
		switch v := m[k].(type) {
		case int:
			return v
		case float64:
			return int(v)
		}
		return 0
	}
	switch eventType {
	case "swarm.started":
		var lines []string
		agents := agentList(p["agents"])
		head := fmt.Sprintf("Swarm (%s) started with %d agents", str("mode"), len(agents))
		if id := str("conversation_id"); id != "" {
			head += ", in the chat /chat?c=" + id
		}
		lines = append(lines, head+".")
		for _, ag := range agents {
			part, _ := ag["part"].(string)
			model, _ := ag["model"].(string)
			line := fmt.Sprintf("Agent %d", num(ag, "agent"))
			if part != "" {
				line += ": " + part
			}
			if model != "" {
				line += " (" + model + ")"
			}
			if status, _ := ag["status"].(string); status == "queued" {
				line += ", waiting for a free slot"
			}
			lines = append(lines, line+".")
		}
		return strings.Join(lines, "\n")
	case "swarm.agent.progress":
		what := str("summary")
		if what == "" {
			what = str("tool")
		}
		if what == "" {
			return ""
		}
		return fmt.Sprintf("Agent %d: %s", num(p, "agent"), what)
	case "swarm.agent.finished":
		line := fmt.Sprintf("Agent %d %s", num(p, "agent"), agentStatus(str("status")))
		if s := str("summary"); s != "" {
			line += ": " + s
		}
		return line
	case "swarm.merging":
		return "Merging what the agents found."
	case "swarm.note":
		return str("note")
	}
	return ""
}

// agentList reads swarm.started's agents, as the swarm sends them or as
// JSON.
func agentList(v any) []map[string]any {
	switch list := v.(type) {
	case []map[string]any:
		return list
	case []any:
		var out []map[string]any
		for _, item := range list {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

func agentStatus(status string) string {
	switch status {
	case "done":
		return "finished"
	case "out_of_budget":
		return "ran out of budget"
	case "":
		return "finished"
	}
	return status
}

// handleSwarm answers a full swarm in Chat Completions' format, its
// progress as reasoning_content and its coordinator chat in the toskar
// extension field.
func (h *Handler) handleSwarm(w http.ResponseWriter, r *http.Request, perms auth.APIKeyPermissions, req chatCompletionRequest, sampling pluginapi.GenerateOptions) {
	stream, conversationID, fail := h.swarmRun(r, perms, req, sampling)
	if fail != nil {
		writeFailure(w, fail)
		return
	}
	extension := func() map[string]any { return map[string]any{"conversation_id": conversationID} }
	if !req.Stream {
		h.writeNonStream(w, stream, req.Model, extension, true)
		return
	}
	sw := &streamWriter{w: w, c: newCompletion(req.Model)}
	if !sw.start() {
		writeError(w, http.StatusInternalServerError, "STREAM_UNSUPPORTED", "streaming not supported")
		return
	}
	includeUsage := req.StreamOptions != nil && req.StreamOptions.IncludeUsage
	h.writeStream(sw, r, stream, req.Model, extension, true, true, includeUsage)
}

// writeFailure writes a raw failure in OpenAI's error shape.
func writeFailure(w http.ResponseWriter, fail *rawFailure) {
	if fail.param != "" {
		writeErrorParam(w, fail.status, fail.code, fail.message, fail.param)
		return
	}
	writeError(w, fail.status, fail.code, fail.message)
}

// swarmRequest is a Messages or Responses request as the full swarm reads
// it: its conversation as text, its controls, and its sampling.
func swarmRequest(model string, body []byte, toskar *assistantOptions, temperature *float64, maxTokens int) chatCompletionRequest {
	return chatCompletionRequest{Model: model, Messages: chatTurns(body), Toskar: toskar, Temperature: temperature, MaxTokens: maxTokens}
}

// messagesSwarm answers a full swarm in Anthropic's Messages format, its
// progress as a thinking block before the answer's text.
func (h *Handler) messagesSwarm(w http.ResponseWriter, r *http.Request, perms auth.APIKeyPermissions, req messagesRequest, body []byte) {
	creq := swarmRequest(req.Model, body, req.Toskar, req.Temperature, req.MaxTokens)
	sampling, err := creq.sampling()
	if err != nil {
		anthropicError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	stream, _, fail := h.swarmRun(r, perms, creq, sampling)
	if fail != nil {
		anthropicError(w, fail.status, anthropicType(fail.status), fail.message)
		return
	}
	id := "msg_" + randomHex(12)
	if !req.Stream {
		var thinking, text strings.Builder
		var metrics *pluginapi.GenerationMetrics
		for c := range stream {
			if c.Error != "" {
				status, _ := h.failure(c.Code, c.Error)
				anthropicError(w, status, anthropicType(status), c.Error)
				return
			}
			thinking.WriteString(c.Reasoning)
			text.WriteString(c.Content)
			if c.Metrics != nil {
				metrics = c.Metrics
			}
		}
		content := []map[string]any{}
		if thinking.Len() > 0 {
			content = append(content, map[string]any{"type": "thinking", "thinking": thinking.String(), "signature": ""})
		}
		content = append(content, map[string]any{"type": "text", "text": text.String()})
		in, out := tokens(metrics)
		writeJSON(w, map[string]any{
			"id": id, "type": "message", "role": "assistant", "model": req.Model,
			"content": content, "stop_reason": "end_turn", "stop_sequence": nil,
			"usage": map[string]int{"input_tokens": in, "output_tokens": out},
		})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	send := func(event string, data any) {
		b, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		if flusher != nil {
			flusher.Flush()
		}
	}
	send("message_start", map[string]any{"type": "message_start", "message": map[string]any{
		"id": id, "type": "message", "role": "assistant", "model": req.Model, "content": []any{},
		"stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 0, "output_tokens": 0},
	}})
	index, open := -1, ""
	start := func(kind string, block map[string]any) {
		if open != "" {
			send("content_block_stop", map[string]any{"type": "content_block_stop", "index": index})
		}
		index++
		open = kind
		send("content_block_start", map[string]any{"type": "content_block_start", "index": index, "content_block": block})
	}
	var metrics *pluginapi.GenerationMetrics
	for c := range stream {
		if c.Error != "" {
			status, _ := h.failure(c.Code, c.Error)
			send("error", map[string]any{"type": "error", "error": map[string]string{"type": anthropicType(status), "message": c.Error}})
			return
		}
		if c.Metrics != nil {
			metrics = c.Metrics
		}
		if c.Reasoning != "" {
			if open != "thinking" {
				start("thinking", map[string]any{"type": "thinking", "thinking": "", "signature": ""})
			}
			send("content_block_delta", map[string]any{"type": "content_block_delta", "index": index, "delta": map[string]any{"type": "thinking_delta", "thinking": c.Reasoning}})
		}
		if c.Content != "" {
			if open != "text" {
				start("text", map[string]any{"type": "text", "text": ""})
			}
			send("content_block_delta", map[string]any{"type": "content_block_delta", "index": index, "delta": map[string]any{"type": "text_delta", "text": c.Content}})
		}
	}
	if open != "" {
		send("content_block_stop", map[string]any{"type": "content_block_stop", "index": index})
	}
	_, out := tokens(metrics)
	send("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]int{"output_tokens": out}})
	send("message_stop", map[string]any{"type": "message_stop"})
}

// responsesSwarm answers a full swarm in OpenAI's Responses format, its
// progress as a reasoning item before the answer's message.
func (h *Handler) responsesSwarm(w http.ResponseWriter, r *http.Request, perms auth.APIKeyPermissions, req responsesRequest, body []byte) {
	creq := swarmRequest(req.Model, body, req.Toskar, req.Temperature, req.MaxOutputTokens)
	sampling, err := creq.sampling()
	if err != nil {
		writeResponsesError(w, http.StatusBadRequest, "invalid_request_error", err.Error(), "")
		return
	}
	stream, _, fail := h.swarmRun(r, perms, creq, sampling)
	if fail != nil {
		writeResponsesError(w, fail.status, fail.code, fail.message, fail.param)
		return
	}
	b := newResponseBuilder(req.Model)
	if req.Stream {
		startResponses(w, b)
	}
	var metrics *pluginapi.GenerationMetrics
	for c := range stream {
		if c.Error != "" {
			status, code := h.failure(c.Code, c.Error)
			if !req.Stream {
				writeResponsesError(w, status, code, c.Error, "")
				return
			}
			failed := b.response("failed")
			failed["error"] = map[string]string{"code": code, "message": c.Error}
			b.send("response.failed", map[string]any{"response": failed})
			return
		}
		if c.Metrics != nil {
			metrics = c.Metrics
		}
		b.reason(c.Reasoning)
		b.text(c.Content)
	}
	b.input, b.output = tokens(metrics)
	if !req.Stream {
		writeJSON(w, b.done())
		return
	}
	finishResponses(b)
}

// tokens are a turn's prompt and answer tokens, or 0 when unknown.
func tokens(m *pluginapi.GenerationMetrics) (int, int) {
	if m == nil {
		return 0, 0
	}
	return m.PromptTokens, m.CompletionTokens
}
