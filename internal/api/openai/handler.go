package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/contextusage"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/runtimes"
	"github.com/yeixio/toskar-core/internal/structured"
	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// ChatService runs chat completions.
type ChatService interface {
	RunChat(ctx context.Context, profileID, conversationID, message string, stream bool, modelID, execution string) (<-chan pluginapi.ChatChunk, error)
}

// Handler serves OpenAI-compatible endpoints.
type Handler struct {
	Profiles *profiles.Manager
	Runtimes *runtimes.Manager
	Chat     ChatService
	Bus      *events.Bus
	Auth     func(r *http.Request) error
	// Permissions authorizes a request and returns what its key may ask of
	// the assistant (§62), and the request's context carrying whose it is
	// (#206). When set, it replaces Auth for chat completions.
	Permissions func(r *http.Request) (auth.APIKeyPermissions, context.Context, error)
	// Specialized lists deployed specialized AIs (sai: ids) for /v1/models.
	Specialized func(ctx context.Context) ([]contracts.Model, error)
	// Pictures keeps a request's pictures while its turn runs (#830), and
	// PictureClient, when set, fetches http(s) ones instead of the
	// internet-only client.
	Pictures      PictureStore
	PictureClient *http.Client
	// Window is a model's context window in tokens, or 0 when unknown, for
	// checking a request fits before generating (#840).
	Window func(ctx context.Context, modelID string) int
	// Grow raises a model installed here to a window of at least need
	// tokens when one fits, and returns it, or 0 (#962).
	Grow func(ctx context.Context, modelID string, need int) int
	// ErrorCode is the stable code for a chat error's text, as chat streams
	// give it (#588), such as CONTEXT_TOO_LONG, or "".
	ErrorCode func(text string) string
}

type chatCompletionRequest struct {
	Model    string                  `json:"model"`
	Messages []pluginapi.ChatMessage `json:"messages"`
	Stream   bool                    `json:"stream"`
	// Temperature (0 to 2) and MaxTokens apply to the calls that write the
	// answer (#70); unset, the model's defaults stand. MaxCompletionTokens
	// is OpenAI's newer name for MaxTokens.
	Temperature         *float64 `json:"temperature"`
	MaxTokens           int      `json:"max_tokens"`
	MaxCompletionTokens int      `json:"max_completion_tokens"`
	// StreamOptions.IncludeUsage ends a stream with a chunk of the tokens
	// it took (#832).
	StreamOptions *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options"`
	// ReasoningEffort is OpenAI's none, minimal, low, medium, or high.
	ReasoningEffort string `json:"reasoning_effort"`
	// IncludeReasoning returns a reasoning model's thinking as
	// reasoning_content beside the answer (#701), as OpenRouter's
	// include_reasoning does. Off, it's left out.
	IncludeReasoning bool `json:"include_reasoning"`
	// Toskar holds the assistant's own controls (§62). Yggdrasil is its name
	// from before the rename, still accepted; Toskar wins when both are sent
	// (#237).
	Toskar    *assistantOptions `json:"toskar"`
	Yggdrasil *assistantOptions `json:"yggdrasil"`
	// ResponseFormat asks for JSON: json_object, or json_schema with a schema (§27).
	ResponseFormat *responseFormat `json:"response_format"`
}

type responseFormat struct {
	Type       string `json:"type"`
	JSONSchema *struct {
		Name   string          `json:"name"`
		Schema json.RawMessage `json:"schema"`
	} `json:"json_schema"`
}

// assistantOptions are the request's assistant controls. Each one can only
// use what the API key allows.
type assistantOptions struct {
	Memory           *bool    `json:"memory"`
	Knowledge        *bool    `json:"knowledge"`
	KnowledgeSources []string `json:"knowledge_sources"`
	Tools            []string `json:"tools"`
	Effort           string   `json:"effort"`
	Placement        string   `json:"placement"`
	// Progress streams the turn's progress and tool activity as chunks with
	// an empty delta and the extension fields.
	Progress bool `json:"progress"`
	// Fallback lets another model answer when the model the request names
	// can't (#871). Off, a named model answers or the request fails.
	Fallback bool `json:"fallback"`
	// ContextWindow is the least window, in tokens, the request wants the
	// model to run with (#962), such as a coding agent's before its first
	// long request: the model starts again with a bigger one if it fits
	// (contract 1.70).
	ContextWindow int `json:"context_window"`
}

// HandleModels lists profiles as OpenAI models.
func (h *Handler) HandleModels(w http.ResponseWriter, r *http.Request) {
	perms := auth.DefaultAPIKeyPermissions()
	switch {
	case h.Permissions != nil:
		p, _, err := h.Permissions(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", err.Error())
			return
		}
		perms = p
	case h.Auth != nil:
		if err := h.Auth(r); err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", err.Error())
			return
		}
	}
	// A key pinned to a profile sees only it (#345).
	if perms.Profile != "" {
		writeJSON(w, map[string]any{"object": "list", "data": []map[string]any{
			{"id": "profile:" + perms.Profile, "object": "model", "owned_by": "yggdrasil"},
		}})
		return
	}
	items, err := h.Profiles.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "MODELS_LIST_FAILED", err.Error())
		return
	}
	// "auto" lets Toskar pick an installed model for each request.
	// owned_by stays "yggdrasil" within contract 1.x: clients may match on
	// it, and it changes with the next major contract version (#237).
	data := make([]map[string]any, 0, len(items)+1)
	data = append(data, map[string]any{"id": "auto", "object": "model", "owned_by": "yggdrasil"})
	for _, p := range items {
		data = append(data, map[string]any{
			"id":       "profile:" + p.ID,
			"object":   "model",
			"owned_by": "yggdrasil",
		})
	}
	if h.Specialized != nil {
		if deployed, err := h.Specialized(r.Context()); err == nil {
			for _, m := range deployed {
				data = append(data, map[string]any{"id": m.ID, "object": "model", "owned_by": "yggdrasil"})
			}
		}
	}
	writeJSON(w, map[string]any{"object": "list", "data": data})
}

// sampling is the request's temperature and token cap for the answer, as
// GenerateOptions: temperature 0 is greedy, which the zero value can't say.
func (req chatCompletionRequest) sampling() (pluginapi.GenerateOptions, error) {
	var o pluginapi.GenerateOptions
	if t := req.Temperature; t != nil {
		if *t < 0 || *t > 2 {
			return o, errors.New("temperature must be between 0 and 2")
		}
		o.Temperature = *t
		if *t == 0 {
			o.Temperature = pluginapi.GreedyTemperature
		}
	}
	o.MaxTokens = req.MaxTokens
	if req.MaxCompletionTokens > 0 {
		o.MaxTokens = req.MaxCompletionTokens
	}
	if o.MaxTokens < 0 || req.MaxCompletionTokens < 0 {
		return o, errors.New("max_tokens must be positive")
	}
	// reasoning_effort "none": a reasoning model answers without thinking
	// first (#840), as OpenAI's reasoning models do.
	o.NoThinking = strings.EqualFold(req.ReasoningEffort, "none")
	// Any other effort asks a model that answers first by default to
	// think (#593).
	o.Thinking = req.ReasoningEffort != "" && !o.NoThinking
	return o, nil
}

// HandleChatCompletions routes profile:ID to orchestrator chat.
func (h *Handler) HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	perms := auth.DefaultAPIKeyPermissions()
	switch {
	case h.Permissions != nil:
		p, ctx, err := h.Permissions(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", err.Error())
			return
		}
		perms = p
		r = r.WithContext(ctx)
	case h.Auth != nil:
		if err := h.Auth(r); err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", err.Error())
			return
		}
	}
	var req chatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}
	sampling, err := req.sampling()
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	profileID := ""
	modelID := ""
	if strings.HasPrefix(req.Model, "profile:") {
		profileID = strings.TrimPrefix(req.Model, "profile:")
	} else {
		// Treat bare ids as a model override (profile falls back to default).
		modelID = req.Model
		profileID = req.Model // also try as profile id when no model files match
	}
	message, history, system := splitMessages(req.Messages)
	if message == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "messages must include a user message")
		return
	}
	opts, execution, effort, err := turnOptions(req, perms)
	if err != nil {
		writeError(w, http.StatusForbidden, "NOT_ALLOWED", err.Error())
		return
	}
	opts.History, opts.System = history, system
	schema, wantJSON, err := jsonFormat(req.ResponseFormat)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_RESPONSE_FORMAT", err.Error())
		return
	}
	if wantJSON {
		opts.System = strings.TrimSpace(opts.System + "\n\n" + jsonInstruction(schema))
	}

	if h.Chat == nil {
		writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "chat not configured")
		return
	}

	// A key pinned to a profile answers with it alone (#345).
	if perms.Profile != "" {
		requested := ""
		if strings.HasPrefix(req.Model, "profile:") {
			requested = profileID
		}
		pinned, err := perms.Pinned(requested, modelID)
		if err != nil {
			writeError(w, http.StatusForbidden, "PROFILE_PINNED", "this key answers only with profile:"+perms.Profile+`; use model "profile:`+perms.Profile+`" or "auto"`)
			return
		}
		profileID, modelID = pinned, ""
		opts.FixedProfile = true
	}
	// Prefer profile when the id matches a known profile; otherwise use as model override.
	if modelID != "" && profileID == modelID {
		if _, err := h.Profiles.Get(r.Context(), profileID); err != nil {
			profileID = ""
		} else {
			modelID = ""
		}
	}

	// A request that names a model means that model: it answers or the
	// request fails, rather than another model answering unsaid (#871).
	// auto and profiles still fall back, and the caller may allow it.
	if o := req.assistant(); modelID != "" && modelID != "auto" && (o == nil || !o.Fallback) {
		opts.ExactModel = true
	}

	// That model's window must hold the messages and the answer asked for:
	// a request that can't fit is refused now, not by the runtime partway
	// (#840). The count is an estimate, so only what's clearly too long is
	// refused here; the runtime still catches the rest.
	// One that needs more than the model runs with gets a bigger window
	// where one fits this computer (#962), and the model starts again with
	// it; the request can ask for a least window too.
	if opts.ExactModel && h.Window != nil {
		window := h.Window(r.Context(), modelID)
		least := 0
		if o := req.assistant(); o != nil {
			least = o.ContextWindow
		}
		if need := max(neededWindow(req, sampling.MaxTokens), least); window > 0 && need > window && h.Grow != nil {
			if grown := h.Grow(r.Context(), modelID, need); grown > 0 {
				window = grown
			}
		}
		if msg, param := tooLong(req, sampling.MaxTokens, window); msg != "" {
			writeErrorParam(w, http.StatusBadRequest, "CONTEXT_TOO_LONG", msg, param)
			return
		}
		if window > 0 && least > window {
			writeErrorParam(w, http.StatusBadRequest, "CONTEXT_TOO_LONG",
				fmt.Sprintf("toskar.context_window asks for %d tokens, but this model runs with at most %d on this computer", least, window), "toskar.context_window")
			return
		}
	}

	// The last user message's pictures, attached as the web chat attaches
	// them (#830), and removed once the answer is done.
	pictures, removePictures, err := h.savePictures(r.Context(), lastUserPictures(req.Messages))
	if err != nil {
		var bad errPicture
		if errors.As(err, &bad) {
			writeError(w, http.StatusBadRequest, "INVALID_IMAGE", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "INVALID_IMAGE", err.Error())
		return
	}
	defer removePictures()

	// The answer's sources, steps, and notice, for the yggdrasil field.
	var meta *contracts.MessageMeta
	var metaMu sync.Mutex
	opts.Meta = func(m *contracts.MessageMeta) {
		metaMu.Lock()
		meta = m
		metaMu.Unlock()
	}
	// One id and time for the whole response, its progress chunks too.
	sw := &streamWriter{w: w, c: newCompletion(profileID)}
	if req.Stream {
		if !sw.start() {
			writeError(w, http.StatusInternalServerError, "STREAM_UNSUPPORTED", "streaming not supported")
			return
		}
		if o := req.assistant(); o != nil && o.Progress {
			opts.Progress = func(eventType string, payload map[string]any) { sw.progress(profileID, eventType, payload) }
		}
	}
	includeUsage := req.StreamOptions != nil && req.StreamOptions.IncludeUsage
	ctx := turnopts.With(r.Context(), opts)
	if len(pictures) > 0 {
		ctx = artifacts.WithAttachments(ctx, pictures)
	}
	if sampling != (pluginapi.GenerateOptions{}) {
		ctx = pluginapi.WithAnswerOptions(ctx, sampling)
	}
	if effort != "" {
		ctx = huginn.WithEffort(ctx, huginn.ParseEffort(effort))
	}
	if wantJSON {
		// The model's reply is constrained to the caller's schema, as they
		// sent it, where the runtime supports it (#831).
		ctx = structured.WithSchema(ctx, schema.Raw)
		// The answer must be data a program can read: checked, repaired,
		// and asked for once more if needed, then sent whole (§27).
		content, metrics, err := h.structuredAnswer(ctx, opts, profileID, message, modelID, execution, schema)
		if err != nil {
			if req.Stream {
				streamError(sw, http.StatusUnprocessableEntity, "INVALID_STRUCTURED_OUTPUT", err.Error())
				return
			}
			writeError(w, http.StatusUnprocessableEntity, "INVALID_STRUCTURED_OUTPUT", err.Error())
			return
		}
		one := make(chan pluginapi.ChatChunk, 1)
		one <- pluginapi.ChatChunk{Content: content, Done: true, Metrics: metrics}
		close(one)
		if !req.Stream {
			h.writeNonStream(w, one, profileID, func() map[string]any { return nil }, false)
			return
		}
		h.writeStream(sw, r, one, profileID, func() map[string]any { return nil }, false, false, includeUsage)
		return
	}
	stream, err := h.Chat.RunChat(ctx, profileID, "", message, req.Stream, modelID, execution)
	if err != nil {
		status, code := h.failure("", err.Error())
		if req.Stream {
			streamError(sw, status, code, err.Error())
			return
		}
		writeError(w, status, code, err.Error())
		return
	}
	extension := func() map[string]any {
		metaMu.Lock()
		defer metaMu.Unlock()
		if meta == nil {
			return nil
		}
		return map[string]any{"sources": meta.Sources, "steps": meta.Steps, "notice": meta.Notice, "files": meta.Files}
	}

	if !req.Stream {
		h.writeNonStream(w, stream, profileID, extension, req.IncludeReasoning)
		return
	}
	h.writeStream(sw, r, stream, profileID, extension, req.assistant() != nil && req.assistant().Progress, req.IncludeReasoning, includeUsage)
}

func (h *Handler) writeNonStream(w http.ResponseWriter, stream <-chan pluginapi.ChatChunk, model string, extension func() map[string]any, includeReasoning bool) {
	var content, reasoning strings.Builder
	var metrics *pluginapi.GenerationMetrics
	for chunk := range stream {
		if chunk.Error != "" {
			status, code := h.failure(chunk.Code, chunk.Error)
			writeError(w, status, code, chunk.Error)
			return
		}
		content.WriteString(chunk.Content)
		reasoning.WriteString(chunk.Reasoning)
		if chunk.Metrics != nil {
			metrics = chunk.Metrics
		}
	}
	message := map[string]any{"role": "assistant", "content": content.String()}
	if includeReasoning && reasoning.Len() > 0 {
		message["reasoning_content"] = reasoning.String()
	}
	out := newCompletion(model).whole(message, metrics)
	if ext := extension(); ext != nil {
		withExtension(out, ext)
	}
	writeJSON(w, out)
}

func (h *Handler) writeStream(sw *streamWriter, r *http.Request, stream <-chan pluginapi.ChatChunk, model string, extension func() map[string]any, progress, includeReasoning, includeUsage bool) {
	c := sw.c
	if c.id == "" {
		c = newCompletion(model)
	}
	var metrics *pluginapi.GenerationMetrics
	for chunk := range stream {
		if chunk.Error != "" {
			status, code := h.failure(chunk.Code, chunk.Error)
			streamError(sw, status, code, chunk.Error)
			return
		}
		if chunk.Metrics != nil {
			metrics = chunk.Metrics
		}
		if includeReasoning && chunk.Reasoning != "" {
			sw.data(c.chunk(map[string]any{"reasoning_content": chunk.Reasoning}, nil))
		}
		if chunk.Content == "" && !chunk.Done {
			continue
		}
		var reason any
		if chunk.Done {
			reason = finish(metrics)
		}
		sw.data(c.chunk(map[string]any{"content": chunk.Content}, reason))
		if h.Bus != nil && chunk.Content != "" {
			h.Bus.PublishFor(r.Context(), events.New(events.ChatToken, map[string]any{"content": chunk.Content}))
		}
	}
	if ext := extension(); progress && ext != nil {
		sw.data(withExtension(c.chunk(map[string]any{}, nil), ext))
	}
	if includeUsage {
		sw.data(c.usageChunk(metrics))
	}
	sw.done()
	if h.Bus != nil {
		h.Bus.PublishFor(r.Context(), events.New(events.ChatComplete, map[string]any{}))
	}
	_ = r.Context().Err()
}

// splitMessages honors the whole message array (§62): the last user
// message is the turn, earlier user and assistant messages are its history,
// and system messages are the caller's instructions.
func splitMessages(messages []pluginapi.ChatMessage) (message string, history []pluginapi.ChatMessage, system string) {
	last := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			last = i
			break
		}
	}
	if last < 0 {
		return "", nil, ""
	}
	var sys []string
	for i, m := range messages {
		switch {
		case m.Role == "system" || m.Role == "developer":
			if s := strings.TrimSpace(m.Content); s != "" {
				sys = append(sys, s)
			}
		case i < last && (m.Role == "user" || m.Role == "assistant") && strings.TrimSpace(m.Content) != "":
			history = append(history, pluginapi.ChatMessage{Role: m.Role, Content: m.Content})
		}
	}
	return messages[last].Content, history, strings.Join(sys, "\n\n")
}

// turnOptions applies the request's controls within the key's permissions.
// Asking for something the key does not allow is refused, not ignored.
func turnOptions(req chatCompletionRequest, perms auth.APIKeyPermissions) (*turnopts.Options, string, string, error) {
	y := req.assistant()
	if y == nil {
		y = &assistantOptions{}
	}
	opts := &turnopts.Options{}
	use := func(level string, asked *bool, what string) (bool, error) {
		switch level {
		case auth.UseNever:
			if asked != nil && *asked {
				return false, fmt.Errorf("this API key may not use %s", what)
			}
			return false, nil
		case auth.UseAlways:
			return asked == nil || *asked, nil
		}
		return asked != nil && *asked, nil
	}
	var err error
	if opts.Memory, err = use(perms.Memory, y.Memory, "memory"); err != nil {
		return nil, "", "", err
	}
	if opts.Knowledge, err = use(perms.Knowledge, y.Knowledge, "connected knowledge"); err != nil {
		return nil, "", "", err
	}
	if len(y.KnowledgeSources) > 0 {
		if perms.Knowledge == auth.UseNever {
			return nil, "", "", fmt.Errorf("this API key may not use connected knowledge")
		}
		opts.KnowledgeSources = y.KnowledgeSources
	}
	switch perms.Tools {
	case auth.ToolsNone:
		if len(y.Tools) > 0 {
			return nil, "", "", fmt.Errorf("this API key may not use tools")
		}
		opts.Tools = []string{}
	case auth.ToolsReadOnly:
		opts.ReadOnlyTools = true
	}
	if y.Tools != nil && opts.Tools == nil {
		opts.Tools = y.Tools
	}
	execution := ""
	switch y.Placement {
	case "":
	case "local", "automatic":
		if !perms.Placement {
			return nil, "", "", fmt.Errorf("this API key may not choose where requests run")
		}
		execution = y.Placement
	default:
		return nil, "", "", fmt.Errorf("placement must be local or automatic")
	}
	effort := y.Effort
	if effort == "" {
		switch strings.ToLower(req.ReasoningEffort) {
		case "none", "minimal", "low":
			effort = "fast"
		case "medium":
			effort = "balanced"
		case "high":
			effort = "thorough"
		}
	}
	return opts, execution, effort, nil
}

// streamWriter writes server-sent events; progress may arrive from several
// goroutines at once.
type streamWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	mu      sync.Mutex
	// c is the response's id, time, and model.
	c completion
}

func (s *streamWriter) start() bool {
	f, ok := s.w.(http.Flusher)
	if !ok {
		return false
	}
	s.flusher = f
	s.w.Header().Set("Content-Type", "text/event-stream")
	s.w.Header().Set("Cache-Control", "no-cache")
	s.w.Header().Set("Connection", "keep-alive")
	s.w.WriteHeader(http.StatusOK)
	f.Flush()
	return true
}

func (s *streamWriter) data(v any) {
	raw, _ := json.Marshal(v)
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintf(s.w, "data: %s\n\n", raw)
	s.flusher.Flush()
}

func (s *streamWriter) done() {
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintf(s.w, "data: [DONE]\n\n")
	s.flusher.Flush()
}

// progressFields are the parts of an event an API client sees.
var progressFields = []string{"tool_id", "query", "steps", "parallel", "index", "step", "status", "model_id", "model_name", "reason", "effort", "issues", "fixed", "remaining", "error", "name"}

// progress writes one event as a chunk with an empty delta, so OpenAI
// clients ignore it and Toskar-aware ones can show it.
func (s *streamWriter) progress(model, eventType string, payload map[string]any) {
	event := map[string]any{"type": eventType}
	for _, k := range progressFields {
		if v, ok := payload[k]; ok {
			event[k] = v
		}
	}
	s.data(withExtension(s.c.chunk(map[string]any{}, nil), map[string]any{"event": event}))
}

// assistant is the request's assistant controls, under either name.
func (r chatCompletionRequest) assistant() *assistantOptions {
	if r.Toskar != nil {
		return r.Toskar
	}
	return r.Yggdrasil
}

// withExtension adds the answer's extension fields to a response or chunk
// as "toskar", and as "yggdrasil", its name from before the rename, which
// clients written for contract 1.x read (#237).
func withExtension(out, ext map[string]any) map[string]any {
	out["toskar"] = ext
	out["yggdrasil"] = ext
	return out
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// failure is the status and stable code for a chat that failed: the code
// it came with, or the one its text is recognized as, as chat streams give
// it (#588), else CHAT_FAILED. A conversation too long for the model is the
// caller's to shorten, so it's a 400, as OpenAI answers one.
func (h *Handler) failure(code, text string) (int, string) {
	if code == "" && h.ErrorCode != nil {
		code = h.ErrorCode(text)
	}
	if code == "" {
		code = "CHAT_FAILED"
	}
	if code == "CONTEXT_TOO_LONG" {
		return http.StatusBadRequest, code
	}
	return http.StatusInternalServerError, code
}

// answerRoom is the room a request without max_tokens is given for its
// answer when its window is sized (#962).
const answerRoom = 2048

// neededWindow is about how many tokens a request's messages and answer take
// together: the messages, estimated, and max_tokens or answerRoom.
func neededWindow(req chatCompletionRequest, maxTokens int) int {
	prompt := 0
	for _, m := range req.Messages {
		prompt += contextusage.Estimate(m.Content)
	}
	if maxTokens <= 0 {
		maxTokens = answerRoom
	}
	return prompt + maxTokens
}

// tooLong says why a request can't fit a model's window of window tokens,
// and which field to change, or "" when it may fit. The messages are
// estimated, so only a request clearly over is refused.
func tooLong(req chatCompletionRequest, maxTokens, window int) (msg, param string) {
	if window <= 0 {
		return "", ""
	}
	if maxTokens >= window {
		return fmt.Sprintf("max_tokens is %d, but this model's context window is %d tokens, which holds the messages and the answer together; ask for fewer", maxTokens, window), "max_tokens"
	}
	prompt := 0
	for _, m := range req.Messages {
		prompt += contextusage.Estimate(m.Content)
	}
	if prompt+maxTokens > window {
		if maxTokens > 0 {
			return fmt.Sprintf("the messages are about %d tokens and max_tokens is %d, more than this model's context window of %d tokens; shorten the messages or ask for fewer", prompt, maxTokens, window), "messages"
		}
		return fmt.Sprintf("the messages are about %d tokens, more than this model's context window of %d tokens; shorten them", prompt, window), "messages"
	}
	return "", ""
}

// writeErrorParam is writeError naming the request field it's about.
func writeErrorParam(w http.ResponseWriter, status int, code, msg, param string) {
	body := errorBody(status, code, msg)
	if e, ok := body["error"].(openAIError); ok {
		e.Param = &param
		body["error"] = e
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError answers OpenAI's error shape (#832): message, type, param,
// and code, which is Toskar's stable code.
func writeError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorBody(status, code, msg))
}

// streamError ends a stream that failed: an error event, then [DONE], so a
// client never waits on a stream that just stops.
func streamError(sw *streamWriter, status int, code, msg string) {
	sw.data(errorBody(status, code, msg))
	sw.done()
}

// Drain closes unused stream on error paths.
func Drain(r io.Reader) { _, _ = io.Copy(io.Discard, r) }
