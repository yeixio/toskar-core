package openai

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/contextusage"
)

// Anthropic's Messages format (#843), for coding agents that speak it:
// POST /v1/messages drives a model on this computer directly, as
// raw:<model> does on /v1/chat/completions. The request is converted to
// the Chat Completions shape llama-server takes, and the answer back,
// streamed as Anthropic's events or not. It's always raw: there is no
// assistant behind it.

// messagesRequest is the part of a Messages request Toskar reads.
type messagesRequest struct {
	Model         string            `json:"model"`
	MaxTokens     int               `json:"max_tokens"`
	System        json.RawMessage   `json:"system"`
	Messages      []anthropicTurn   `json:"messages"`
	Tools         []anthropicTool   `json:"tools"`
	ToolChoice    *anthropicChoice  `json:"tool_choice"`
	StopSequences []string          `json:"stop_sequences"`
	Temperature   *float64          `json:"temperature"`
	TopP          *float64          `json:"top_p"`
	TopK          *int              `json:"top_k"`
	Stream        bool              `json:"stream"`
	Toskar        *assistantOptions `json:"toskar"`
}

type anthropicTurn struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// anthropicBlock is one content block, of any type.
type anthropicBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	// tool_addition, a tool added partway, as Claude Code sends in a
	// system turn
	Tool *struct {
		Definition anthropicTool `json:"definition"`
	} `json:"tool,omitempty"`
	// image
	Source *struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
		URL       string `json:"url"`
	} `json:"source,omitempty"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicChoice struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

// blocks reads content that is a string or a list of blocks.
func blocks(raw json.RawMessage) ([]anthropicBlock, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return []anthropicBlock{{Type: "text", Text: s}}, nil
	}
	var out []anthropicBlock
	err := json.Unmarshal(raw, &out)
	return out, err
}

// blockText is the text of blocks, joined: a system prompt or a tool's
// result given as blocks.
func blockText(bs []anthropicBlock) string {
	var parts []string
	for _, b := range bs {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// chatMessages converts a Messages request to Chat Completions messages:
// the system prompt first; a user turn's tool_result blocks as tool
// messages, before its text and pictures; an assistant turn's text as its
// content and its tool_use blocks as tool_calls.
func (m messagesRequest) chatMessages() ([]map[string]any, error) {
	var out []map[string]any
	if sys, err := blocks(m.System); err != nil {
		return nil, fmt.Errorf("system must be a string or text blocks")
	} else if text := blockText(sys); text != "" {
		out = append(out, map[string]any{"role": "system", "content": text})
	}
	for i, turn := range m.Messages {
		bs, err := blocks(turn.Content)
		if err != nil {
			return nil, fmt.Errorf("messages[%d].content must be a string or a list of blocks", i)
		}
		switch turn.Role {
		case "user":
			var parts []map[string]any
			for _, b := range bs {
				switch b.Type {
				case "tool_result":
					content, _ := blocks(b.Content)
					text := blockText(content)
					if b.IsError {
						text = "Error: " + text
					}
					out = append(out, map[string]any{"role": "tool", "tool_call_id": b.ToolUseID, "content": text})
				case "text":
					parts = append(parts, map[string]any{"type": "text", "text": b.Text})
				case "image":
					if url := imageURL(b); url != "" {
						parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}})
					}
				}
			}
			if len(parts) > 0 {
				out = append(out, map[string]any{"role": "user", "content": parts})
			}
		case "assistant":
			msg := map[string]any{"role": "assistant"}
			var text []string
			var calls []map[string]any
			for _, b := range bs {
				switch b.Type {
				case "text":
					text = append(text, b.Text)
				case "tool_use":
					args := string(b.Input)
					if strings.TrimSpace(args) == "" {
						args = "{}"
					}
					calls = append(calls, map[string]any{"id": b.ID, "type": "function", "function": map[string]any{"name": b.Name, "arguments": args}})
				}
			}
			msg["content"] = strings.Join(text, "")
			if len(calls) > 0 {
				msg["tool_calls"] = calls
			}
			out = append(out, msg)
		case "system":
			// A system turn partway, as Claude Code sends one: its text is
			// instructions, and its tool_addition blocks are tools (added to
			// the request's in tools()).
			if text := blockText(bs); text != "" {
				out = append(out, map[string]any{"role": "system", "content": text})
			}
		default:
			return nil, fmt.Errorf("messages[%d].role must be user, assistant, or system", i)
		}
	}
	return out, nil
}

func imageURL(b anthropicBlock) string {
	if b.Source == nil {
		return ""
	}
	if b.Source.Type == "url" {
		return b.Source.URL
	}
	if b.Source.Data != "" {
		return "data:" + b.Source.MediaType + ";base64," + b.Source.Data
	}
	return ""
}

// chatBody is the Chat Completions request llama-server takes for m.
func (m messagesRequest) chatBody() ([]byte, error) {
	msgs, err := m.chatMessages()
	if err != nil {
		return nil, err
	}
	body := map[string]any{"messages": msgs, "max_tokens": m.MaxTokens, "stream": m.Stream}
	if m.Stream {
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	if len(m.StopSequences) > 0 {
		body["stop"] = m.StopSequences
	}
	if m.Temperature != nil {
		body["temperature"] = *m.Temperature
	}
	if m.TopP != nil {
		body["top_p"] = *m.TopP
	}
	if m.TopK != nil {
		body["top_k"] = *m.TopK
	}
	if all := m.tools(); len(all) > 0 && (m.ToolChoice == nil || m.ToolChoice.Type != "none") {
		var tools []map[string]any
		for _, t := range all {
			params := t.InputSchema
			if len(params) == 0 {
				params = json.RawMessage(`{"type":"object"}`)
			}
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{"name": t.Name, "description": t.Description, "parameters": params}})
		}
		body["tools"] = tools
		if c := m.ToolChoice; c != nil {
			switch c.Type {
			case "any":
				body["tool_choice"] = "required"
			case "tool":
				body["tool_choice"] = map[string]any{"type": "function", "function": map[string]any{"name": c.Name}}
			default:
				body["tool_choice"] = "auto"
			}
		}
	}
	return json.Marshal(body)
}

// tools are the request's tools and those its system turns add.
func (m messagesRequest) tools() []anthropicTool {
	out := append([]anthropicTool(nil), m.Tools...)
	for _, turn := range m.Messages {
		if turn.Role != "system" {
			continue
		}
		bs, _ := blocks(turn.Content)
		for _, b := range bs {
			if b.Type == "tool_addition" && b.Tool != nil && b.Tool.Definition.Name != "" {
				out = append(out, b.Tool.Definition)
			}
		}
	}
	return out
}

// estimate is about how many tokens m's messages take, for its window.
func (m messagesRequest) estimate() int {
	n := 0
	if sys, err := blocks(m.System); err == nil {
		n += contextusage.Estimate(blockText(sys))
	}
	for _, turn := range m.Messages {
		bs, _ := blocks(turn.Content)
		for _, b := range bs {
			n += contextusage.Estimate(b.Text) + contextusage.Estimate(string(b.Input))
			if b.Type == "tool_result" {
				c, _ := blocks(b.Content)
				n += contextusage.Estimate(blockText(c))
			}
		}
	}
	for _, t := range m.tools() {
		n += contextusage.Estimate(t.Name+" "+t.Description) + contextusage.Estimate(string(t.InputSchema))
	}
	return n
}

// HandleMessages serves POST /v1/messages (#843).
func (h *Handler) HandleMessages(w http.ResponseWriter, r *http.Request) {
	perms, r, ok := h.messagesAuth(w, r)
	if !ok {
		return
	}
	var req messagesRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBytes)).Decode(&req); err != nil {
		anthropicError(w, http.StatusBadRequest, "invalid_request_error", "invalid request body")
		return
	}
	if req.MaxTokens <= 0 {
		anthropicError(w, http.StatusBadRequest, "invalid_request_error", "max_tokens is required")
		return
	}
	body, err := req.chatBody()
	if err != nil {
		anthropicError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	need := req.estimate() + min(req.MaxTokens, rawAnswerRoom)
	if req.Toskar != nil {
		need = max(need, req.Toskar.ContextWindow)
	}
	endpoint, done, fail := h.rawReady(r.Context(), perms, strings.TrimPrefix(req.Model, RawPrefix), need)
	if fail != nil {
		anthropicError(w, fail.status, anthropicType(fail.status), fail.message)
		return
	}
	defer done()
	resp, err := rawPost(r.Context(), endpoint, body)
	if err != nil {
		if r.Context().Err() == nil {
			anthropicError(w, http.StatusBadGateway, "api_error", "the model didn't answer: "+err.Error())
		}
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		anthropicError(w, resp.StatusCode, anthropicType(resp.StatusCode), runtimeMessage(raw))
		return
	}
	id := "msg_" + randomHex(12)
	if !req.Stream {
		var chat chatAnswer
		if err := json.NewDecoder(resp.Body).Decode(&chat); err != nil {
			anthropicError(w, http.StatusBadGateway, "api_error", "the model's answer wasn't readable")
			return
		}
		writeJSON(w, chat.message(id, req.Model))
		return
	}
	streamMessages(w, resp.Body, id, req.Model)
}

// HandleCountTokens serves POST /v1/messages/count_tokens: about how many
// tokens a Messages request's input takes, as agents ask before sending.
func (h *Handler) HandleCountTokens(w http.ResponseWriter, r *http.Request) {
	_, r, ok := h.messagesAuth(w, r)
	if !ok {
		return
	}
	var req messagesRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBytes)).Decode(&req); err != nil {
		anthropicError(w, http.StatusBadRequest, "invalid_request_error", "invalid request body")
		return
	}
	writeJSON(w, map[string]int{"input_tokens": req.estimate()})
}

// messagesAuth is the request's permissions, as for chat completions, or
// false after writing Anthropic's error.
func (h *Handler) messagesAuth(w http.ResponseWriter, r *http.Request) (auth.APIKeyPermissions, *http.Request, bool) {
	perms, r, err := h.permissionsFor(r)
	if err != nil {
		anthropicError(w, http.StatusUnauthorized, "authentication_error", err.Error())
		return perms, r, false
	}
	return perms, r, true
}

// permissionsFor is what a raw format's request may do, and the request
// carrying whose it is, as for chat completions.
func (h *Handler) permissionsFor(r *http.Request) (auth.APIKeyPermissions, *http.Request, error) {
	if h.Permissions != nil {
		p, ctx, err := h.Permissions(r)
		if err != nil {
			return auth.APIKeyPermissions{}, r, err
		}
		return p, r.WithContext(ctx), nil
	}
	if h.Auth != nil {
		if err := h.Auth(r); err != nil {
			return auth.APIKeyPermissions{}, r, err
		}
	}
	return auth.APIKeyPermissions{}, r, nil
}

// chatAnswer is the part of a Chat Completions answer a Messages answer
// is made from.
type chatAnswer struct {
	Choices []struct {
		Message struct {
			Content   string     `json:"content"`
			ToolCalls []toolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type toolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// message is the Messages answer for c.
func (c chatAnswer) message(id, model string) map[string]any {
	content := []map[string]any{}
	finish := ""
	if len(c.Choices) > 0 {
		choice := c.Choices[0]
		finish = choice.FinishReason
		if choice.Message.Content != "" {
			content = append(content, map[string]any{"type": "text", "text": choice.Message.Content})
		}
		for _, call := range choice.Message.ToolCalls {
			content = append(content, map[string]any{"type": "tool_use", "id": toolUseID(call.ID), "name": call.Function.Name, "input": toolInput(call.Function.Arguments)})
		}
	}
	return map[string]any{
		"id": id, "type": "message", "role": "assistant", "model": model,
		"content": content, "stop_reason": stopReason(finish), "stop_sequence": nil,
		"usage": map[string]int{"input_tokens": c.Usage.PromptTokens, "output_tokens": c.Usage.CompletionTokens},
	}
}

// stopReason is Anthropic's word for a Chat Completions finish_reason.
func stopReason(finish string) string {
	switch finish {
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	}
	return "end_turn"
}

// toolInput is a tool call's arguments as the object Anthropic's tool_use
// carries; arguments that aren't a JSON object become an empty one.
func toolInput(args string) json.RawMessage {
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(args), &obj) != nil {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(args)
}

// toolUseID is a tool call's id, or a new one when the model gave none.
func toolUseID(id string) string {
	if id != "" {
		return id
	}
	return "toolu_" + randomHex(12)
}

// streamMessages turns llama-server's Chat Completions stream into
// Anthropic's events: message_start; for each text run or tool call,
// content_block_start, its deltas (text_delta or input_json_delta), and
// content_block_stop; then message_delta with the stop reason and usage,
// and message_stop.
func streamMessages(w http.ResponseWriter, body io.Reader, id, model string) {
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
		"id": id, "type": "message", "role": "assistant", "model": model, "content": []any{},
		"stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 0, "output_tokens": 0},
	}})
	index := -1
	open := ""   // "text" or "tool", the block open now
	call := -1   // the open tool call's index in the stream
	finish := "" // the finish_reason, once given
	output := 0
	start := func(block map[string]any) {
		index++
		send("content_block_start", map[string]any{"type": "content_block_start", "index": index, "content_block": block})
	}
	stop := func() {
		if open != "" {
			send("content_block_stop", map[string]any{"type": "content_block_stop", "index": index})
			open = ""
		}
	}
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for sc.Scan() {
		data, ok := bytes.CutPrefix(sc.Bytes(), []byte("data: "))
		if !ok || bytes.Equal(data, []byte("[DONE]")) {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string     `json:"content"`
					ToolCalls []toolCall `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(data, &chunk) != nil {
			continue
		}
		if chunk.Usage != nil {
			output = chunk.Usage.CompletionTokens
		}
		for _, choice := range chunk.Choices {
			if text := choice.Delta.Content; text != "" {
				if open != "text" {
					stop()
					start(map[string]any{"type": "text", "text": ""})
					open = "text"
				}
				send("content_block_delta", map[string]any{"type": "content_block_delta", "index": index, "delta": map[string]any{"type": "text_delta", "text": text}})
			}
			for _, tc := range choice.Delta.ToolCalls {
				if open != "tool" || tc.Index != call {
					stop()
					call = tc.Index
					start(map[string]any{"type": "tool_use", "id": toolUseID(tc.ID), "name": tc.Function.Name, "input": map[string]any{}})
					open = "tool"
				}
				if tc.Function.Arguments != "" {
					send("content_block_delta", map[string]any{"type": "content_block_delta", "index": index, "delta": map[string]any{"type": "input_json_delta", "partial_json": tc.Function.Arguments}})
				}
			}
			if choice.FinishReason != nil && *choice.FinishReason != "" {
				finish = *choice.FinishReason
			}
		}
	}
	stop()
	send("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stopReason(finish), "stop_sequence": nil}, "usage": map[string]int{"output_tokens": output}})
	send("message_stop", map[string]any{"type": "message_stop"})
}

// anthropicError writes Anthropic's error shape.
func anthropicError(w http.ResponseWriter, status int, kind, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "error", "error": map[string]string{"type": kind, "message": message}})
}

// anthropicType is Anthropic's error type for a status.
func anthropicType(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_error"
	case http.StatusNotFound:
		return "not_found_error"
	case http.StatusBadRequest:
		return "invalid_request_error"
	}
	return "api_error"
}

// runtimeMessage is the message in llama-server's error answer, or its
// text.
func runtimeMessage(raw []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &e) == nil && e.Error.Message != "" {
		return e.Error.Message
	}
	return strings.TrimSpace(string(raw))
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
