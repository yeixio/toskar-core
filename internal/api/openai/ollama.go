package openai

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Ollama's API (#1356), so apps that offer only Ollama, such as Home
// Assistant's Ollama integration, Obsidian plugins, and editor plugins, can
// use Toskar. Each request is turned into a Chat Completions request and
// answered by HandleChatCompletions, so Auto, profiles, keys, and
// permissions behave as they do on /v1/chat/completions; the answer is
// turned back into Ollama's shape, a JSON object per line when streaming.
// A client's own tools reach the model on raw:<model>, as on Chat
// Completions.

// OllamaVersion is the Ollama API version Toskar answers as: the one whose
// endpoints and fields these handlers cover.
const OllamaVersion = "0.12.0"

// Embed embeds texts with the installed embedding model, for /api/embed,
// and names it; it's nil, or errors, when there is none.
type Embed func(r *http.Request, texts []string) ([][]float32, string, error)

// ollamaOptions are Ollama's sampling options that have a Chat
// Completions field.
type ollamaOptions struct {
	Temperature *float64 `json:"temperature"`
	TopP        float64  `json:"top_p"`
	TopK        int      `json:"top_k"`
	MinP        float64  `json:"min_p"`
	Seed        *int64   `json:"seed"`
	NumPredict  int      `json:"num_predict"`
	Stop        []string `json:"stop"`
}

type ollamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	Images    []string         `json:"images,omitempty"`
	Thinking  string           `json:"thinking,omitempty"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
	ToolName  string           `json:"tool_name,omitempty"`
}

type ollamaToolCall struct {
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	// Stream is on unless it's false, as in Ollama.
	Stream  *bool             `json:"stream"`
	Format  json.RawMessage   `json:"format"`
	Options ollamaOptions     `json:"options"`
	Tools   []json.RawMessage `json:"tools"`
	Think   json.RawMessage   `json:"think"`
}

type ollamaGenerateRequest struct {
	Model   string          `json:"model"`
	Prompt  string          `json:"prompt"`
	System  string          `json:"system"`
	Images  []string        `json:"images"`
	Stream  *bool           `json:"stream"`
	Format  json.RawMessage `json:"format"`
	Options ollamaOptions   `json:"options"`
	Think   json.RawMessage `json:"think"`
}

// ollamaModel is a model name as Toskar knows it: ":latest", which Ollama
// clients add to a name without a tag, is dropped.
func ollamaModel(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "auto"
	}
	return strings.TrimSuffix(name, ":latest")
}

// imageData is an Ollama image, base64 with no prefix, as a data URL.
func imageData(b64 string) string {
	if strings.HasPrefix(b64, "data:") {
		return b64
	}
	mime := "image/png"
	switch {
	case strings.HasPrefix(b64, "/9j/"):
		mime = "image/jpeg"
	case strings.HasPrefix(b64, "R0lG"):
		mime = "image/gif"
	case strings.HasPrefix(b64, "UklG"):
		mime = "image/webp"
	}
	return "data:" + mime + ";base64," + b64
}

// chatMessage is an Ollama message in Chat Completions' shape.
func (m ollamaMessage) chatMessage(i int) map[string]any {
	out := map[string]any{"role": m.Role}
	if len(m.Images) > 0 {
		parts := []map[string]any{{"type": "text", "text": m.Content}}
		for _, img := range m.Images {
			parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]string{"url": imageData(img)}})
		}
		out["content"] = parts
	} else {
		out["content"] = m.Content
	}
	if len(m.ToolCalls) > 0 {
		calls := make([]map[string]any, 0, len(m.ToolCalls))
		for j, c := range m.ToolCalls {
			args := string(c.Function.Arguments)
			if args == "" || args == "null" {
				args = "{}"
			}
			calls = append(calls, map[string]any{"id": toolCallID(i, j), "type": "function",
				"function": map[string]string{"name": c.Function.Name, "arguments": args}})
		}
		out["tool_calls"] = calls
	}
	if m.Role == "tool" && m.ToolName != "" {
		out["name"] = m.ToolName
	}
	return out
}

func toolCallID(message, call int) string {
	return "call_" + strconv.Itoa(message) + "_" + strconv.Itoa(call)
}

// chatBody is the Chat Completions request for an Ollama one.
func chatBody(model string, messages []map[string]any, stream bool, format json.RawMessage, o ollamaOptions, tools []json.RawMessage, think json.RawMessage) ([]byte, error) {
	body := map[string]any{"model": ollamaModel(model), "messages": messages, "stream": stream}
	if stream {
		body["stream_options"] = map[string]bool{"include_usage": true}
	}
	if o.Temperature != nil {
		body["temperature"] = *o.Temperature
	}
	if o.TopP > 0 {
		body["top_p"] = o.TopP
	}
	if o.TopK > 0 {
		body["top_k"] = o.TopK
	}
	if o.MinP > 0 {
		body["min_p"] = o.MinP
	}
	if o.Seed != nil {
		body["seed"] = *o.Seed
	}
	if o.NumPredict > 0 {
		body["max_tokens"] = o.NumPredict
	}
	if len(o.Stop) > 0 {
		body["stop"] = o.Stop
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	// format is "json" or a JSON schema.
	if f := bytes.TrimSpace(format); len(f) > 0 && !bytes.Equal(f, []byte("null")) && !bytes.Equal(f, []byte(`""`)) {
		if bytes.Equal(f, []byte(`"json"`)) {
			body["response_format"] = map[string]any{"type": "json_object"}
		} else {
			body["response_format"] = map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "answer", "schema": json.RawMessage(f)}}
		}
	}
	// think: false turns a reasoning model's thinking off; on, it's returned.
	switch t := strings.Trim(string(bytes.TrimSpace(think)), `"`); t {
	case "false":
		body["reasoning_effort"] = "none"
	case "true", "low", "medium", "high":
		body["include_reasoning"] = true
		if t != "true" {
			body["reasoning_effort"] = t
		}
	}
	return json.Marshal(body)
}

// HandleOllamaChat serves POST /api/chat.
func (h *Handler) HandleOllamaChat(w http.ResponseWriter, r *http.Request) {
	var req ollamaChatRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBytes)).Decode(&req); err != nil {
		ollamaError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	stream := req.Stream == nil || *req.Stream
	if len(req.Messages) == 0 {
		// Ollama loads the model for an empty chat.
		ollamaWrite(w, stream, ollamaDone(req.Model, "load", map[string]any{"message": map[string]any{"role": "assistant", "content": ""}}))
		return
	}
	messages := make([]map[string]any, 0, len(req.Messages))
	for i, m := range req.Messages {
		messages = append(messages, m.chatMessage(i))
	}
	body, err := chatBody(req.Model, messages, stream, req.Format, req.Options, req.Tools, req.Think)
	if err != nil {
		ollamaError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.ollamaTurn(w, r, body, req.Model, stream, false)
}

// HandleOllamaGenerate serves POST /api/generate: a prompt, with an
// optional system prompt and pictures.
func (h *Handler) HandleOllamaGenerate(w http.ResponseWriter, r *http.Request) {
	var req ollamaGenerateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBytes)).Decode(&req); err != nil {
		ollamaError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	stream := req.Stream == nil || *req.Stream
	if strings.TrimSpace(req.Prompt) == "" && len(req.Images) == 0 {
		ollamaWrite(w, stream, ollamaDone(req.Model, "load", map[string]any{"response": ""}))
		return
	}
	var messages []map[string]any
	if req.System != "" {
		messages = append(messages, map[string]any{"role": "system", "content": req.System})
	}
	messages = append(messages, ollamaMessage{Role: "user", Content: req.Prompt, Images: req.Images}.chatMessage(0))
	body, err := chatBody(req.Model, messages, stream, req.Format, req.Options, nil, req.Think)
	if err != nil {
		ollamaError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.ollamaTurn(w, r, body, req.Model, stream, true)
}

// ollamaTurn answers a Chat Completions body through HandleChatCompletions
// and writes the answer in Ollama's shape: chat's message, or generate's
// response.
func (h *Handler) ollamaTurn(w http.ResponseWriter, r *http.Request, body []byte, model string, stream, generate bool) {
	inner := r.Clone(r.Context())
	inner.Body = io.NopCloser(bytes.NewReader(body))
	inner.ContentLength = int64(len(body))
	inner.Header.Set("Content-Type", "application/json")
	started := time.Now()
	out := &ollamaAnswer{w: w, model: model, generate: generate, stream: stream, started: started}
	if !stream {
		rec := httptest.NewRecorder()
		h.HandleChatCompletions(rec, inner)
		if rec.Code >= 300 {
			ollamaError(w, rec.Code, errorText(rec.Body.Bytes()))
			return
		}
		var chat struct {
			Usage struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
			Choices []struct {
				Message struct {
					Content          string     `json:"content"`
					ReasoningContent string     `json:"reasoning_content"`
					ToolCalls        []toolCall `json:"tool_calls"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &chat); err != nil || len(chat.Choices) == 0 {
			ollamaError(w, http.StatusBadGateway, "the model's answer wasn't readable")
			return
		}
		c := chat.Choices[0]
		out.text.WriteString(c.Message.Content)
		out.thinking.WriteString(c.Message.ReasoningContent)
		out.calls = c.Message.ToolCalls
		out.finish = c.FinishReason
		out.prompt, out.eval = chat.Usage.PromptTokens, chat.Usage.CompletionTokens
		writeJSON(w, out.final(true))
		return
	}
	// Streaming: the SSE HandleChatCompletions writes is read as it comes
	// and each chunk becomes a line.
	pr, pw := io.Pipe()
	sse := &pipeWriter{header: http.Header{}, pipe: pw, status: make(chan int, 1)}
	go func() {
		h.HandleChatCompletions(sse, inner)
		sse.WriteHeader(http.StatusOK)
		_ = pw.Close()
	}()
	status := <-sse.status
	if status >= 300 {
		raw, _ := io.ReadAll(pr)
		ollamaError(w, status, errorText(raw))
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	out.flusher, _ = w.(http.Flusher)
	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for sc.Scan() {
		data, ok := bytes.CutPrefix(sc.Bytes(), []byte("data: "))
		if !ok || bytes.Equal(data, []byte("[DONE]")) {
			continue
		}
		out.chunk(data)
	}
	_, _ = io.Copy(io.Discard, pr)
	if !out.failed {
		out.line(out.final(false))
	}
}

// ollamaAnswer builds Ollama's answer from Chat Completions chunks.
type ollamaAnswer struct {
	w        http.ResponseWriter
	flusher  http.Flusher
	model    string
	generate bool
	stream   bool
	started  time.Time

	text, thinking strings.Builder
	calls          []toolCall
	finish         string
	prompt, eval   int
	// failed is set once an error line was sent; nothing follows it.
	failed bool
}

func (a *ollamaAnswer) chunk(data []byte) {
	var chunk struct {
		Choices []struct {
			Delta struct {
				Content          string     `json:"content"`
				ReasoningContent string     `json:"reasoning_content"`
				ToolCalls        []toolCall `json:"tool_calls"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &chunk) != nil {
		return
	}
	if chunk.Error != nil && chunk.Error.Message != "" {
		a.line(map[string]any{"error": chunk.Error.Message})
		a.failed = true
		return
	}
	if chunk.Usage != nil {
		a.prompt, a.eval = chunk.Usage.PromptTokens, chunk.Usage.CompletionTokens
	}
	for _, c := range chunk.Choices {
		// Tool calls stream in pieces; Ollama sends each one whole, at the end.
		for _, tc := range c.Delta.ToolCalls {
			for len(a.calls) <= tc.Index {
				a.calls = append(a.calls, toolCall{Index: len(a.calls)})
			}
			call := &a.calls[tc.Index]
			if tc.ID != "" {
				call.ID = tc.ID
			}
			if tc.Function.Name != "" {
				call.Function.Name = tc.Function.Name
			}
			call.Function.Arguments += tc.Function.Arguments
		}
		if c.FinishReason != nil && *c.FinishReason != "" {
			a.finish = *c.FinishReason
		}
		if c.Delta.Content == "" && c.Delta.ReasoningContent == "" {
			continue
		}
		a.line(a.piece(c.Delta.Content, c.Delta.ReasoningContent))
	}
}

// piece is one streamed line of text or thinking.
func (a *ollamaAnswer) piece(text, thinking string) map[string]any {
	out := map[string]any{"model": a.model, "created_at": time.Now().UTC().Format(time.RFC3339Nano), "done": false}
	if a.generate {
		out["response"] = text
		if thinking != "" {
			out["thinking"] = thinking
		}
	} else {
		msg := map[string]any{"role": "assistant", "content": text}
		if thinking != "" {
			msg["thinking"] = thinking
		}
		out["message"] = msg
	}
	return out
}

// final is the last object: the whole answer when it wasn't streamed, and
// the tool calls, why it stopped, and its counts.
func (a *ollamaAnswer) final(whole bool) map[string]any {
	text, thinking := "", ""
	if whole {
		text, thinking = a.text.String(), a.thinking.String()
	}
	out := a.piece(text, thinking)
	out["done"] = true
	reason := "stop"
	if a.finish == "length" {
		reason = "length"
	}
	out["done_reason"] = reason
	out["total_duration"] = time.Since(a.started).Nanoseconds()
	out["prompt_eval_count"] = a.prompt
	out["eval_count"] = a.eval
	if len(a.calls) > 0 && !a.generate {
		calls := make([]map[string]any, 0, len(a.calls))
		for _, c := range a.calls {
			calls = append(calls, map[string]any{"function": map[string]any{"name": c.Function.Name, "arguments": toolInput(c.Function.Arguments)}})
		}
		out["message"].(map[string]any)["tool_calls"] = calls
	}
	return out
}

func (a *ollamaAnswer) line(v any) {
	b, _ := json.Marshal(v)
	_, _ = a.w.Write(append(b, '\n'))
	if a.flusher != nil {
		a.flusher.Flush()
	}
}

// ollamaDone is a finished answer with nothing in it, as for loading a model.
func ollamaDone(model, reason string, extra map[string]any) map[string]any {
	out := map[string]any{"model": model, "created_at": time.Now().UTC().Format(time.RFC3339Nano), "done": true, "done_reason": reason}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

// ollamaWrite writes one object, as a line when streaming.
func ollamaWrite(w http.ResponseWriter, stream bool, v any) {
	if !stream {
		writeJSON(w, v)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	b, _ := json.Marshal(v)
	_, _ = w.Write(append(b, '\n'))
}

// pipeWriter is the ResponseWriter HandleChatCompletions writes a stream
// to: its status is sent once, and the body goes down the pipe.
type pipeWriter struct {
	header http.Header
	pipe   *io.PipeWriter
	once   sync.Once
	status chan int
}

func (p *pipeWriter) Header() http.Header { return p.header }
func (p *pipeWriter) WriteHeader(code int) {
	p.once.Do(func() { p.status <- code })
}
func (p *pipeWriter) Write(b []byte) (int, error) {
	p.WriteHeader(http.StatusOK)
	return p.pipe.Write(b)
}
func (p *pipeWriter) Flush() {}

// errorText is the message in a Chat Completions error answer.
func errorText(raw []byte) string {
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

// ollamaError writes Ollama's error shape.
func ollamaError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// ollamaModels is what /v1/models lists for this request's key.
func (h *Handler) ollamaModels(r *http.Request) ([]string, int, string) {
	rec := httptest.NewRecorder()
	h.HandleModels(rec, r)
	if rec.Code >= 300 {
		return nil, rec.Code, errorText(rec.Body.Bytes())
	}
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	ids := make([]string, 0, len(list.Data))
	for _, m := range list.Data {
		ids = append(ids, m.ID)
	}
	return ids, http.StatusOK, ""
}

func ollamaDetails() map[string]any {
	return map[string]any{"format": "gguf", "family": "toskar", "families": []string{"toskar"}, "parameter_size": "", "quantization_level": ""}
}

// HandleOllamaTags serves GET /api/tags: Auto, each profile, and what else
// the key may ask for, as Ollama models.
func (h *Handler) HandleOllamaTags(w http.ResponseWriter, r *http.Request) {
	ids, status, msg := h.ollamaModels(r)
	if status >= 300 {
		ollamaError(w, status, msg)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	models := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		models = append(models, map[string]any{"name": id, "model": id, "modified_at": now, "size": 0, "digest": "", "details": ollamaDetails()})
	}
	writeJSON(w, map[string]any{"models": models})
}

// HandleOllamaShow serves POST /api/show: what a model can do.
func (h *Handler) HandleOllamaShow(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		ollamaError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := ollamaModel(req.Model + req.Name)
	ids, status, msg := h.ollamaModels(r)
	if status >= 300 {
		ollamaError(w, status, msg)
		return
	}
	known := false
	for _, id := range ids {
		known = known || id == name
	}
	if !known {
		ollamaError(w, http.StatusNotFound, "model '"+name+"' not found")
		return
	}
	// Auto and profiles see pictures by choosing a model that does, and
	// use Toskar's own tools; an app's tools reach a raw model.
	caps := []string{"completion", "vision"}
	if strings.HasPrefix(name, RawPrefix) || strings.HasPrefix(name, SwarmPrefix) {
		caps = []string{"completion", "tools"}
	}
	writeJSON(w, map[string]any{
		"modelfile": "", "parameters": "", "template": "", "details": ollamaDetails(),
		"model_info": map[string]any{}, "capabilities": caps, "modified_at": time.Now().UTC().Format(time.RFC3339),
	})
}

// HandleOllamaPS serves GET /api/ps. Toskar starts and stops models on its
// own, so none is listed as held for a client.
func (h *Handler) HandleOllamaPS(w http.ResponseWriter, r *http.Request) {
	if _, status, msg := h.ollamaModels(r); status >= 300 {
		ollamaError(w, status, msg)
		return
	}
	writeJSON(w, map[string]any{"models": []any{}})
}

// HandleOllamaVersion serves GET /api/version.
func (h *Handler) HandleOllamaVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]string{"version": OllamaVersion})
}

// HandleOllamaManage answers Ollama's model management (pull, push, create,
// copy, delete): models are managed in Toskar.
func (h *Handler) HandleOllamaManage(w http.ResponseWriter, _ *http.Request) {
	ollamaError(w, http.StatusForbidden, "Toskar manages its models itself: install or remove them on its Models page. Use auto, or a name from /api/tags.")
}

// HandleOllamaEmbed serves POST /api/embed (and the older /api/embeddings,
// one prompt) with the installed embedding model.
func (h *Handler) HandleOllamaEmbed(w http.ResponseWriter, r *http.Request) {
	_, r, err := h.permissionsFor(r)
	if err != nil {
		ollamaError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var req struct {
		Model  string          `json:"model"`
		Input  json.RawMessage `json:"input"`
		Prompt string          `json:"prompt"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBytes)).Decode(&req); err != nil {
		ollamaError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var texts []string
	if one := ""; json.Unmarshal(req.Input, &one) == nil && one != "" {
		texts = []string{one}
	} else if json.Unmarshal(req.Input, &texts) != nil || len(texts) == 0 {
		texts = nil
	}
	legacy := len(texts) == 0 && req.Prompt != ""
	if legacy {
		texts = []string{req.Prompt}
	}
	if len(texts) == 0 {
		ollamaError(w, http.StatusBadRequest, "input is required")
		return
	}
	if h.Embed == nil {
		ollamaError(w, http.StatusNotImplemented, "embeddings aren't available")
		return
	}
	vectors, model, err := h.Embed(r, texts)
	if err != nil {
		ollamaError(w, http.StatusBadRequest, err.Error())
		return
	}
	if legacy && len(vectors) > 0 {
		writeJSON(w, map[string]any{"embedding": vectors[0]})
		return
	}
	writeJSON(w, map[string]any{"model": model, "embeddings": vectors})
}
