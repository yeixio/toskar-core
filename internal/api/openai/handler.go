package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/yeixio/yggdrasil-core/internal/events"
	"github.com/yeixio/yggdrasil-core/internal/profiles"
	"github.com/yeixio/yggdrasil-core/internal/runtimes"
	"github.com/yeixio/yggdrasil-core/pkg/contracts"
	"github.com/yeixio/yggdrasil-core/pkg/pluginapi"
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
	// Specialized lists deployed specialized AIs (sai: ids) for /v1/models.
	Specialized func(ctx context.Context) ([]contracts.Model, error)
}

type chatCompletionRequest struct {
	Model       string                  `json:"model"`
	Messages    []pluginapi.ChatMessage `json:"messages"`
	Stream      bool                    `json:"stream"`
	Temperature float64                 `json:"temperature"`
	MaxTokens   int                     `json:"max_tokens"`
}

// HandleModels lists profiles as OpenAI models.
func (h *Handler) HandleModels(w http.ResponseWriter, r *http.Request) {
	if h.Auth != nil {
		if err := h.Auth(r); err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", err.Error())
			return
		}
	}
	items, err := h.Profiles.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "MODELS_LIST_FAILED", err.Error())
		return
	}
	// "auto" lets Yggdrasil pick an installed model for each request.
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

// HandleChatCompletions routes profile:ID to orchestrator chat.
func (h *Handler) HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if h.Auth != nil {
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
	profileID := ""
	modelID := ""
	if strings.HasPrefix(req.Model, "profile:") {
		profileID = strings.TrimPrefix(req.Model, "profile:")
	} else {
		// Treat bare ids as a model override (profile falls back to default).
		modelID = req.Model
		profileID = req.Model // also try as profile id when no model files match
	}
	message := lastUserMessage(req.Messages)
	if message == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "messages must include a user message")
		return
	}

	if h.Chat == nil {
		writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "chat not configured")
		return
	}

	// Prefer profile when the id matches a known profile; otherwise use as model override.
	if modelID != "" && profileID == modelID {
		if _, err := h.Profiles.Get(r.Context(), profileID); err != nil {
			profileID = ""
		} else {
			modelID = ""
		}
	}

	stream, err := h.Chat.RunChat(r.Context(), profileID, "", message, req.Stream, modelID, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "CHAT_FAILED", err.Error())
		return
	}

	if !req.Stream {
		h.writeNonStream(w, stream, profileID)
		return
	}
	h.writeStream(w, r, stream, profileID)
}

func (h *Handler) writeNonStream(w http.ResponseWriter, stream <-chan pluginapi.ChatChunk, model string) {
	var content strings.Builder
	for chunk := range stream {
		if chunk.Error != "" {
			writeError(w, http.StatusInternalServerError, "CHAT_FAILED", chunk.Error)
			return
		}
		content.WriteString(chunk.Content)
	}
	writeJSON(w, map[string]any{
		"id":      "chatcmpl-ygg",
		"object":  "chat.completion",
		"model":   model,
		"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": content.String()}, "finish_reason": "stop"}},
	})
}

func (h *Handler) writeStream(w http.ResponseWriter, r *http.Request, stream <-chan pluginapi.ChatChunk, model string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "STREAM_UNSUPPORTED", "streaming not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	id := "chatcmpl-ygg"
	for chunk := range stream {
		if chunk.Error != "" {
			payload, _ := json.Marshal(map[string]any{"error": map[string]any{"message": chunk.Error}})
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
			return
		}
		if chunk.Content == "" && !chunk.Done {
			continue
		}
		data, _ := json.Marshal(map[string]any{
			"id":      id,
			"object":  "chat.completion.chunk",
			"model":   model,
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": chunk.Content}, "finish_reason": finishReason(chunk.Done)}},
		})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		if h.Bus != nil && chunk.Content != "" {
			h.Bus.Publish(events.New(events.ChatToken, map[string]any{"content": chunk.Content}))
		}
	}
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
	if h.Bus != nil {
		h.Bus.Publish(events.New(events.ChatComplete, map[string]any{}))
	}
	_ = r.Context().Err()
}

func finishReason(done bool) any {
	if done {
		return "stop"
	}
	return nil
}

func lastUserMessage(messages []pluginapi.ChatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(contracts.APIError{Error: contracts.ErrorBody{Code: code, Message: msg}})
}

// Drain closes unused stream on error paths.
func Drain(r io.Reader) { _, _ = io.Copy(io.Discard, r) }
