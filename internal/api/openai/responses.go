package openai

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/internal/contextusage"
)

// OpenAI's Responses format (#843), for coding agents that speak it:
// POST /v1/responses drives a model on this computer directly, as
// raw:<model> does on /v1/chat/completions. The request is converted to
// the Chat Completions shape llama-server takes, and the answer back,
// streamed as the response.* events or not. It's always raw, and keeps no
// state: an agent sends the whole conversation each time, as one that
// sets store to false does.

// responsesRequest is the part of a Responses request Toskar reads.
type responsesRequest struct {
	Model              string            `json:"model"`
	Input              json.RawMessage   `json:"input"`
	Instructions       string            `json:"instructions"`
	Tools              []responsesTool   `json:"tools"`
	ToolChoice         json.RawMessage   `json:"tool_choice"`
	MaxOutputTokens    int               `json:"max_output_tokens"`
	Temperature        *float64          `json:"temperature"`
	TopP               *float64          `json:"top_p"`
	Stream             bool              `json:"stream"`
	PreviousResponseID string            `json:"previous_response_id"`
	ParallelToolCalls  *bool             `json:"parallel_tool_calls"`
	Toskar             *assistantOptions `json:"toskar"`
}

type responsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// responsesItem is one input item: a message, a function call, or a
// function call's output.
type responsesItem struct {
	Type    string          `json:"type"`
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	// function_call and function_call_output
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Output    string `json:"output"`
}

type responsesPart struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	ImageURL string `json:"image_url"`
}

// items reads input that is a string or a list of items.
func (q responsesRequest) items() ([]responsesItem, error) {
	raw := bytes.TrimSpace(q.Input)
	if len(raw) == 0 || string(raw) == "null" {
		return nil, fmt.Errorf("input is required")
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return []responsesItem{{Type: "message", Role: "user", Content: raw}}, nil
	}
	var out []responsesItem
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("input must be a string or a list of items")
	}
	return out, nil
}

// parts reads a message's content: a string or a list of parts.
func parts(raw json.RawMessage) []responsesPart {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if raw[0] == '"' {
		var s string
		_ = json.Unmarshal(raw, &s)
		return []responsesPart{{Type: "input_text", Text: s}}
	}
	var out []responsesPart
	_ = json.Unmarshal(raw, &out)
	return out
}

func partsText(ps []responsesPart) string {
	var texts []string
	for _, p := range ps {
		if p.Type == "input_text" || p.Type == "output_text" || p.Type == "text" {
			texts = append(texts, p.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// chatMessages converts the input to Chat Completions messages: the
// instructions, and system and developer messages, as system messages; a
// run of function_call items as one assistant turn's tool_calls, with the
// assistant's text just before them; a function_call_output as a tool
// message. Reasoning items are left out.
func (q responsesRequest) chatMessages() ([]map[string]any, error) {
	items, err := q.items()
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	if s := strings.TrimSpace(q.Instructions); s != "" {
		out = append(out, map[string]any{"role": "system", "content": s})
	}
	for i, it := range items {
		kind := it.Type
		if kind == "" && it.Role != "" {
			kind = "message"
		}
		switch kind {
		case "message":
			ps := parts(it.Content)
			switch it.Role {
			case "system", "developer":
				out = append(out, map[string]any{"role": "system", "content": partsText(ps)})
			case "assistant":
				out = append(out, map[string]any{"role": "assistant", "content": partsText(ps)})
			case "user":
				var content []map[string]any
				for _, p := range ps {
					switch p.Type {
					case "input_text", "text":
						content = append(content, map[string]any{"type": "text", "text": p.Text})
					case "input_image":
						if p.ImageURL != "" {
							content = append(content, map[string]any{"type": "image_url", "image_url": map[string]any{"url": p.ImageURL}})
						}
					}
				}
				out = append(out, map[string]any{"role": "user", "content": content})
			default:
				return nil, fmt.Errorf("input[%d].role must be user, assistant, system, or developer", i)
			}
		case "function_call":
			call := map[string]any{"id": it.CallID, "type": "function", "function": map[string]any{"name": it.Name, "arguments": orEmptyObject(it.Arguments)}}
			if n := len(out); n > 0 && out[n-1]["role"] == "assistant" {
				calls, _ := out[n-1]["tool_calls"].([]map[string]any)
				out[n-1]["tool_calls"] = append(calls, call)
			} else {
				out = append(out, map[string]any{"role": "assistant", "content": "", "tool_calls": []map[string]any{call}})
			}
		case "function_call_output":
			out = append(out, map[string]any{"role": "tool", "tool_call_id": it.CallID, "content": it.Output})
		case "reasoning":
		default:
			return nil, fmt.Errorf("input[%d].type %q isn't one Toskar reads", i, it.Type)
		}
	}
	return out, nil
}

func orEmptyObject(s string) string {
	if strings.TrimSpace(s) == "" {
		return "{}"
	}
	return s
}

// chatBody is the Chat Completions request llama-server takes for q.
func (q responsesRequest) chatBody() ([]byte, error) {
	msgs, err := q.chatMessages()
	if err != nil {
		return nil, err
	}
	body := map[string]any{"messages": msgs, "stream": q.Stream}
	if q.Stream {
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	if q.MaxOutputTokens > 0 {
		body["max_tokens"] = q.MaxOutputTokens
	}
	if q.Temperature != nil {
		body["temperature"] = *q.Temperature
	}
	if q.TopP != nil {
		body["top_p"] = *q.TopP
	}
	if q.ParallelToolCalls != nil {
		body["parallel_tool_calls"] = *q.ParallelToolCalls
	}
	var tools []map[string]any
	for _, t := range q.Tools {
		if t.Type != "function" {
			continue
		}
		params := t.Parameters
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object"}`)
		}
		tools = append(tools, map[string]any{"type": "function", "function": map[string]any{"name": t.Name, "description": t.Description, "parameters": params}})
	}
	if len(tools) > 0 {
		body["tools"] = tools
		if choice, ok := chatToolChoice(q.ToolChoice); ok {
			body["tool_choice"] = choice
		}
	}
	return json.Marshal(body)
}

// chatToolChoice is a Responses tool_choice in Chat Completions' shape:
// auto, required, and none as they are, and {"type":"function","name"} as
// a named function.
func chatToolChoice(raw json.RawMessage) (any, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil, false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, true
	}
	var named struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &named) == nil && named.Type == "function" && named.Name != "" {
		return map[string]any{"type": "function", "function": map[string]any{"name": named.Name}}, true
	}
	return nil, false
}

// estimate is about how many tokens q's input takes, for its window.
func (q responsesRequest) estimate() int {
	n := contextusage.Estimate(q.Instructions) + contextusage.Estimate(string(q.Input))
	for _, t := range q.Tools {
		n += contextusage.Estimate(t.Name+" "+t.Description) + contextusage.Estimate(string(t.Parameters))
	}
	return n
}

// HandleResponses serves POST /v1/responses (#843).
func (h *Handler) HandleResponses(w http.ResponseWriter, r *http.Request) {
	perms, r, err := h.permissionsFor(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", err.Error())
		return
	}
	var req responsesRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBytes)).Decode(&req); err != nil {
		writeResponsesError(w, http.StatusBadRequest, "invalid_request_error", "invalid request body", "")
		return
	}
	if req.PreviousResponseID != "" {
		writeResponsesError(w, http.StatusBadRequest, "invalid_request_error", "Toskar keeps no responses to continue from: send the whole conversation as input, as with store set to false", "previous_response_id")
		return
	}
	body, err := req.chatBody()
	if err != nil {
		writeResponsesError(w, http.StatusBadRequest, "invalid_request_error", err.Error(), "input")
		return
	}
	out := req.MaxOutputTokens
	if out <= 0 {
		out = answerRoom
	}
	need := req.estimate() + min(out, rawAnswerRoom)
	if req.Toskar != nil {
		need = max(need, req.Toskar.ContextWindow)
	}
	endpoint, done, fail := h.rawReady(r.Context(), perms, strings.TrimPrefix(req.Model, RawPrefix), need)
	if fail != nil {
		writeResponsesError(w, fail.status, fail.code, fail.message, fail.param)
		return
	}
	defer done()
	resp, err := rawPost(r.Context(), endpoint, body)
	if err != nil {
		if r.Context().Err() == nil {
			writeResponsesError(w, http.StatusBadGateway, "RUNTIME_ERROR", "the model didn't answer: "+err.Error(), "")
		}
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		writeResponsesError(w, resp.StatusCode, "RUNTIME_ERROR", runtimeMessage(raw), "")
		return
	}
	b := newResponseBuilder(req.Model)
	if !req.Stream {
		var chat chatAnswer
		if err := json.NewDecoder(resp.Body).Decode(&chat); err != nil {
			writeResponsesError(w, http.StatusBadGateway, "RUNTIME_ERROR", "the model's answer wasn't readable", "")
			return
		}
		if len(chat.Choices) > 0 {
			c := chat.Choices[0]
			b.text(c.Message.Content)
			for _, call := range c.Message.ToolCalls {
				b.call(call.Index, call.ID, call.Function.Name, call.Function.Arguments)
			}
			b.finish = c.FinishReason
		}
		b.input, b.output = chat.Usage.PromptTokens, chat.Usage.CompletionTokens
		writeJSON(w, b.done())
		return
	}
	streamResponses(w, resp.Body, b)
}

// responseBuilder gathers a response's output items as the answer comes,
// and, given emit, sends the response.* events for each.
type responseBuilder struct {
	id, model     string
	created       int64
	items         []map[string]any
	open          int // the index of the item being written, or -1
	calls         map[int]int
	finish        string
	input, output int
	emit          func(event string, data map[string]any)
}

func newResponseBuilder(model string) *responseBuilder {
	return &responseBuilder{id: "resp_" + randomHex(12), model: model, created: time.Now().Unix(), open: -1, calls: map[int]int{}}
}

func (b *responseBuilder) send(event string, data map[string]any) {
	if b.emit != nil {
		data["type"] = event
		b.emit(event, data)
	}
}

// text adds text to the message item being written, opening one first.
func (b *responseBuilder) text(t string) {
	if t == "" {
		return
	}
	if b.open < 0 || b.items[b.open]["type"] != "message" {
		b.close()
		item := map[string]any{"type": "message", "id": "msg_" + randomHex(12), "status": "in_progress", "role": "assistant", "content": []any{}}
		b.items = append(b.items, item)
		b.open = len(b.items) - 1
		b.send("response.output_item.added", map[string]any{"output_index": b.open, "item": item})
		b.send("response.content_part.added", map[string]any{"item_id": item["id"], "output_index": b.open, "content_index": 0, "part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}}})
		item["text"] = ""
	}
	item := b.items[b.open]
	item["text"] = item["text"].(string) + t
	b.send("response.output_text.delta", map[string]any{"item_id": item["id"], "output_index": b.open, "content_index": 0, "delta": t})
}

// call adds to the function call item for the answer's index-th tool
// call, opening one when it's new.
func (b *responseBuilder) call(index int, id, name, args string) {
	at, ok := b.calls[index]
	if !ok {
		b.close()
		if id == "" {
			id = "call_" + randomHex(12)
		}
		item := map[string]any{"type": "function_call", "id": "fc_" + randomHex(12), "call_id": id, "name": name, "arguments": "", "status": "in_progress"}
		b.items = append(b.items, item)
		at = len(b.items) - 1
		b.calls[index], b.open = at, at
		b.send("response.output_item.added", map[string]any{"output_index": at, "item": item})
	}
	item := b.items[at]
	if args != "" {
		item["arguments"] = item["arguments"].(string) + args
		b.send("response.function_call_arguments.delta", map[string]any{"item_id": item["id"], "output_index": at, "delta": args})
	}
}

// close finishes the item being written.
func (b *responseBuilder) close() {
	if b.open < 0 {
		return
	}
	item := b.items[b.open]
	item["status"] = "completed"
	switch item["type"] {
	case "message":
		text, _ := item["text"].(string)
		delete(item, "text")
		part := map[string]any{"type": "output_text", "text": text, "annotations": []any{}}
		item["content"] = []any{part}
		b.send("response.output_text.done", map[string]any{"item_id": item["id"], "output_index": b.open, "content_index": 0, "text": text})
		b.send("response.content_part.done", map[string]any{"item_id": item["id"], "output_index": b.open, "content_index": 0, "part": part})
	case "function_call":
		b.send("response.function_call_arguments.done", map[string]any{"item_id": item["id"], "output_index": b.open, "arguments": item["arguments"]})
	}
	b.send("response.output_item.done", map[string]any{"output_index": b.open, "item": item})
	b.open = -1
}

// response is the response object, in_progress until done.
func (b *responseBuilder) response(status string) map[string]any {
	items := b.items
	if items == nil {
		items = []map[string]any{}
	}
	out := map[string]any{
		"id": b.id, "object": "response", "created_at": b.created, "status": status, "model": b.model,
		"output": items, "error": nil, "incomplete_details": nil,
	}
	if status != "in_progress" {
		out["usage"] = map[string]int{"input_tokens": b.input, "output_tokens": b.output, "total_tokens": b.input + b.output}
	}
	if status == "incomplete" {
		out["incomplete_details"] = map[string]string{"reason": "max_output_tokens"}
	}
	return out
}

// done closes the last item and returns the finished response.
func (b *responseBuilder) done() map[string]any {
	b.close()
	status := "completed"
	if b.finish == "length" {
		status = "incomplete"
	}
	return b.response(status)
}

// streamResponses turns llama-server's Chat Completions stream into the
// response.* events: response.created and response.in_progress; for each
// message or function call, output_item.added, its deltas, and its
// .done events; then response.completed, or response.incomplete at the
// token limit. Each event carries a sequence_number.
func streamResponses(w http.ResponseWriter, body io.Reader, b *responseBuilder) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	seq := 0
	b.emit = func(event string, data map[string]any) {
		data["sequence_number"] = seq
		seq++
		raw, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw)
		if flusher != nil {
			flusher.Flush()
		}
	}
	b.send("response.created", map[string]any{"response": b.response("in_progress")})
	b.send("response.in_progress", map[string]any{"response": b.response("in_progress")})
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
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(data, &chunk) != nil {
			continue
		}
		if chunk.Usage != nil {
			b.input, b.output = chunk.Usage.PromptTokens, chunk.Usage.CompletionTokens
		}
		for _, c := range chunk.Choices {
			b.text(c.Delta.Content)
			for _, tc := range c.Delta.ToolCalls {
				b.call(tc.Index, tc.ID, tc.Function.Name, tc.Function.Arguments)
			}
			if c.FinishReason != nil && *c.FinishReason != "" {
				b.finish = *c.FinishReason
			}
		}
	}
	final := b.done()
	event := "response.completed"
	if final["status"] == "incomplete" {
		event = "response.incomplete"
	}
	b.send(event, map[string]any{"response": final})
}

// writeResponsesError writes OpenAI's error shape, as the Responses API
// answers one.
func writeResponsesError(w http.ResponseWriter, status int, code, message, param string) {
	if param != "" {
		writeErrorParam(w, status, code, message, param)
		return
	}
	writeError(w, status, code, message)
}
