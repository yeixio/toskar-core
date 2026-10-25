package openai

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// What every chat completion carries, as OpenAI's do (#832): an id of its
// own, when it was made, why the model stopped, and the tokens it took.
// The streamed and the whole answer are built here alike, so the two can't
// drift.

// completion is one response's id, creation time, and model.
type completion struct {
	id      string
	created int64
	model   string
}

func newCompletion(model string) completion {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return completion{id: "chatcmpl-" + hex.EncodeToString(b), created: time.Now().Unix(), model: model}
}

// whole is a non-streamed response.
func (c completion) whole(message map[string]any, m *pluginapi.GenerationMetrics) map[string]any {
	return map[string]any{
		"id": c.id, "object": "chat.completion", "created": c.created, "model": c.model,
		"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish(m)}},
		"usage":   usage(m),
	}
}

// chunk is a streamed chunk with delta, and finishReason when it's the last.
func (c completion) chunk(delta map[string]any, finishReason any) map[string]any {
	return map[string]any{
		"id": c.id, "object": "chat.completion.chunk", "created": c.created, "model": c.model,
		"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finishReason}},
	}
}

// usageChunk is the stream's last chunk when the request asked for usage
// (stream_options.include_usage): no choices, and the tokens.
func (c completion) usageChunk(m *pluginapi.GenerationMetrics) map[string]any {
	return map[string]any{
		"id": c.id, "object": "chat.completion.chunk", "created": c.created, "model": c.model,
		"choices": []any{}, "usage": usage(m),
	}
}

// finish is why the model stopped: length when it reached its token limit,
// so an answer cut short isn't taken for a whole one, and stop otherwise.
func finish(m *pluginapi.GenerationMetrics) string {
	if m != nil && m.FinishReason == "length" {
		return "length"
	}
	return "stop"
}

// usage is OpenAI's usage, from what the runtime measured.
func usage(m *pluginapi.GenerationMetrics) map[string]any {
	if m == nil {
		m = &pluginapi.GenerationMetrics{}
	}
	total := m.TotalTokens
	if total == 0 {
		total = m.PromptTokens + m.CompletionTokens
	}
	return map[string]any{
		"prompt_tokens":             m.PromptTokens,
		"completion_tokens":         m.CompletionTokens,
		"total_tokens":              total,
		"prompt_tokens_details":     map[string]any{"cached_tokens": m.CachedTokens},
		"completion_tokens_details": map[string]any{"reasoning_tokens": m.ReasoningTokens},
	}
}

// openAIError is OpenAI's error shape: message, type, param, and code,
// with Toskar's stable code as code.
type openAIError struct {
	Message string  `json:"message"`
	Type    string  `json:"type"`
	Param   *string `json:"param"`
	Code    string  `json:"code"`
}

// errorParams are the request fields some errors are about.
var errorParams = map[string]string{
	"INVALID_RESPONSE_FORMAT": "response_format",
	"INVALID_IMAGE":           "messages",
	"PROFILE_PINNED":          "model",
}

func errorBody(status int, code, msg string) map[string]any {
	e := openAIError{Message: msg, Type: errorType(status), Code: code}
	if p, ok := errorParams[code]; ok {
		e.Param = &p
	}
	return map[string]any{"error": e}
}

func errorType(status int) string {
	switch {
	case status == http.StatusUnauthorized:
		return "authentication_error"
	case status == http.StatusForbidden:
		return "permission_error"
	case status == http.StatusTooManyRequests:
		return "rate_limit_error"
	case status >= 500:
		return "server_error"
	}
	return "invalid_request_error"
}
