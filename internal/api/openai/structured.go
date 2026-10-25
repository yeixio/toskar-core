package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yeixio/toskar-core/internal/structured"
	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// anyObject is json_object's schema.
var anyObject = structured.MustCompile(`{"type":"object"}`)

// jsonFormat reads OpenAI's response_format. It reports whether JSON was
// asked for and the schema to check it against: the caller's, whole
// (#831), refused when it isn't valid JSON Schema or is too big.
func jsonFormat(f *responseFormat) (*structured.FullSchema, bool, error) {
	if f == nil || f.Type == "" || f.Type == "text" {
		return nil, false, nil
	}
	switch f.Type {
	case "json_object":
		return anyObject, true, nil
	case "json_schema":
		if f.JSONSchema == nil || len(f.JSONSchema.Schema) == 0 {
			return nil, false, fmt.Errorf("response_format.json_schema.schema is required")
		}
		s, err := structured.Compile(f.JSONSchema.Schema)
		if err != nil {
			return nil, false, fmt.Errorf("response_format.json_schema.schema: %w", err)
		}
		return s, true, nil
	}
	return nil, false, fmt.Errorf("response_format.type must be text, json_object, or json_schema")
}

// jsonInstruction tells the model the shape to answer in, with the
// caller's schema as they sent it.
func jsonInstruction(s *structured.FullSchema) string {
	if s == nil || s == anyObject {
		return "Reply with only a JSON object, no other text."
	}
	return "Reply with only JSON that matches this JSON Schema, no other text: " + string(s.Raw)
}

// structuredAnswer runs the turn, then checks its JSON against the schema
// after safe repairs. An answer that still does not fit is sent back once
// with the problems named. It returns compact JSON, or why none fit.
func (h *Handler) structuredAnswer(ctx context.Context, opts *turnopts.Options, profileID, message, modelID, execution string, s *structured.FullSchema) (string, *pluginapi.GenerationMetrics, error) {
	text, used, err := h.collect(ctx, profileID, message, modelID, execution)
	if err != nil {
		return "", nil, err
	}
	r := structured.ParseFull(text, s)
	if !r.OK() {
		retry := *opts
		retry.History = append(append([]pluginapi.ChatMessage(nil), opts.History...),
			pluginapi.ChatMessage{Role: "user", Content: message},
			pluginapi.ChatMessage{Role: "assistant", Content: text})
		var again *pluginapi.GenerationMetrics
		text, again, err = h.collect(turnopts.With(ctx, &retry), profileID, structured.FixPromptFull(r, s), modelID, execution)
		if err != nil {
			return "", nil, err
		}
		used = addUsage(used, again)
		r = structured.ParseFull(text, s)
	}
	if !r.OK() {
		return "", nil, fmt.Errorf("the model did not produce JSON that fits: %s", strings.TrimSpace(strings.ReplaceAll(structured.Describe(r.Issues), "\n", "; ")))
	}
	raw, err := json.Marshal(r.Value)
	return string(raw), used, err
}

// addUsage is both calls' tokens, and why the last one stopped.
func addUsage(a, b *pluginapi.GenerationMetrics) *pluginapi.GenerationMetrics {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	}
	sum := *b
	sum.PromptTokens += a.PromptTokens
	sum.CompletionTokens += a.CompletionTokens
	sum.TotalTokens += a.TotalTokens
	sum.CachedTokens += a.CachedTokens
	sum.ReasoningTokens += a.ReasoningTokens
	return &sum
}

// collect runs one turn and returns its text.
func (h *Handler) collect(ctx context.Context, profileID, message, modelID, execution string) (string, *pluginapi.GenerationMetrics, error) {
	ch, err := h.Chat.RunChat(ctx, profileID, "", message, false, modelID, execution)
	if err != nil {
		return "", nil, err
	}
	var b strings.Builder
	var metrics *pluginapi.GenerationMetrics
	for chunk := range ch {
		if chunk.Error != "" {
			return "", nil, fmt.Errorf("%s", chunk.Error)
		}
		b.WriteString(chunk.Content)
		if chunk.Metrics != nil {
			metrics = chunk.Metrics
		}
	}
	return b.String(), metrics, nil
}
