package llamacpp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Client streams OpenAI-compatible chat completions from llama-server.
type Client struct {
	http *http.Client
}

// NewClient creates a llama-server HTTP client.
func NewClient() *Client {
	return &Client{http: &http.Client{}}
}

// Chat implements pluginapi.Generator. A reply llama.cpp can't read, as
// when the model calls a tool it wasn't offered, is asked for once more,
// with a note saying which tools there are (#755).
func (c *Client) Chat(ctx context.Context, req pluginapi.ChatRequest) (<-chan pluginapi.ChatChunk, error) {
	again := func() (<-chan pluginapi.ChatChunk, error) { return c.chat(ctx, withToolNote(req), nil) }
	return c.chat(ctx, req, again)
}

// chat sends req; again, when set, asks once more after a format mismatch.
func (c *Client) chat(ctx context.Context, req pluginapi.ChatRequest, again func() (<-chan pluginapi.ChatChunk, error)) (<-chan pluginapi.ChatChunk, error) {
	if req.ModelEndpoint == "" {
		return nil, fmt.Errorf("model endpoint required")
	}
	body := map[string]any{
		// Text that spells a control token stays text (#829).
		"messages": escapeControl(req.ModelEndpoint, req.Messages),
		"stream":   req.Stream,
	}
	if t, ok := pluginapi.SamplingTemperature(req.Temperature); ok {
		body["temperature"] = t
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if len(req.Tools) > 0 {
		body["tools"] = req.Tools
	}
	if len(req.ResponseSchema) > 0 {
		// llama-server turns the schema into a grammar, so the reply is
		// valid JSON of that shape (§27).
		body["response_format"] = map[string]any{"type": "json_object", "schema": req.ResponseSchema}
	}
	if req.NoThinking {
		// Thinking off (#840): the chat template's own switch where it has
		// one (Qwen 3), and a thinking budget of none for the rest, such as
		// DeepSeek R1's distills, whose templates always open a <think>.
		body["chat_template_kwargs"] = map[string]any{"enable_thinking": false}
		body["thinking_budget_tokens"] = 0
	}
	lora, err := loraScales(req.ModelEndpoint, req.Adapter)
	if err != nil {
		return nil, err
	}
	if lora != nil {
		body["lora"] = lora
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(req.ModelEndpoint, "/") + "/v1/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	started := time.Now()
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if again != nil && formatMismatch(string(b)) {
			return again()
		}
		return nil, fmt.Errorf("llama-server error %d: %s", resp.StatusCode, string(b))
	}

	ch := make(chan pluginapi.ChatChunk, 16)
	if !req.Stream {
		go c.readNonStream(resp, ch, started, again, req.NoThinking)
		return ch, nil
	}
	go c.readStream(resp, ch, started, again, req.NoThinking)
	return ch, nil
}

// formatMismatch is llama.cpp's chat parser rejecting a reply it can't
// read, as when a model calls a tool it wasn't offered: Muse Glimmer calls
// places.search, from its training, in a turn with no tools (#755).
func formatMismatch(message string) bool {
	return strings.Contains(message, "does not match the expected")
}

// withToolNote is req to ask again after a format mismatch: the person's
// last message says which tools there are, or that there are none. A
// system line didn't stop it; a note in the message did (#755).
func withToolNote(req pluginapi.ChatRequest) pluginapi.ChatRequest {
	note := "(No tools are available for this message. Answer directly from what you know.)"
	if names := toolNames(req.Tools); len(names) > 0 {
		note = "(Only these tools are available for this message: " + strings.Join(names, ", ") + ". Use one of them, or answer directly.)"
	}
	msgs := append([]pluginapi.ChatMessage(nil), req.Messages...)
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			msgs[i].Content = strings.TrimRight(msgs[i].Content, "\n") + "\n\n" + note
			break
		}
	}
	req.Messages = msgs
	return req
}

// toolNames are the names of the tools a request offers.
func toolNames(tools []map[string]any) []string {
	var names []string
	for _, t := range tools {
		if fn, ok := t["function"].(map[string]any); ok {
			if name, ok := fn["name"].(string); ok && name != "" {
				names = append(names, name)
			}
		}
	}
	return names
}

// pipe sends what asking again answers, in place of the reply that failed.
func pipe(ch chan<- pluginapi.ChatChunk, again func() (<-chan pluginapi.ChatChunk, error), failed string) {
	next, err := again()
	if err != nil {
		ch <- pluginapi.ChatChunk{Error: failed, Done: true}
		return
	}
	for chunk := range next {
		ch <- chunk
	}
}

func (c *Client) readNonStream(resp *http.Response, ch chan<- pluginapi.ChatChunk, started time.Time, again func() (<-chan pluginapi.ChatChunk, error), noThinking bool) {
	defer close(ch)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		ch <- pluginapi.ChatChunk{Error: err.Error(), Done: true}
		return
	}
	if message, failed := streamDataError(string(body)); failed {
		if again != nil && formatMismatch(message) {
			pipe(ch, again, message)
			return
		}
		ch <- pluginapi.ChatChunk{Error: message, Done: true}
		return
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
				// What a reasoning model thought first (#701).
				Reasoning string `json:"reasoning_content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage   *usagePayload   `json:"usage"`
		Timings *timingsPayload `json:"timings"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		ch <- pluginapi.ChatChunk{Error: err.Error(), Done: true}
		return
	}
	content, finish, thought := "", "", ""
	if len(out.Choices) > 0 {
		content, finish, thought = out.Choices[0].Message.Content, out.Choices[0].FinishReason, out.Choices[0].Message.Reasoning
		if noThinking {
			content = dropClosedThink(content)
		}
		if thought != "" {
			ch <- pluginapi.ChatChunk{Reasoning: thought}
		}
	}
	metrics := mergeServerMetrics(out.Usage, out.Timings, started, time.Time{}, time.Now(), content)
	metrics.FinishReason = finish
	if thought != "" {
		metrics.ReasoningTokens = reasoningTokens(resp, thought)
	}
	ch <- pluginapi.ChatChunk{Content: content, Done: true, Metrics: metrics}
}

func (c *Client) readStream(resp *http.Response, ch chan<- pluginapi.ChatChunk, started time.Time, again func() (<-chan pluginapi.ChatChunk, error), noThinking bool) {
	defer close(ch)
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var firstToken time.Time
	var content strings.Builder
	var usage *usagePayload
	var timings *timingsPayload
	sentDone := false
	// llama-server streams a token a chunk, so the thinking's chunks are
	// its tokens.
	finish, thoughtTokens := "", 0
	// With thinking off, the answer can start with the </think> that ended
	// the thinking it skipped; it's held back until it's clear.
	lead := &closedThink{done: !noThinking}

	flushDone := func() {
		if sentDone {
			return
		}
		sentDone = true
		metrics := mergeServerMetrics(usage, timings, started, firstToken, time.Now(), content.String())
		metrics.FinishReason, metrics.ReasoningTokens = finish, thoughtTokens
		ch <- pluginapi.ChatChunk{Done: true, Metrics: metrics}
	}

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			if rest := lead.flush(); rest != "" {
				content.WriteString(rest)
				ch <- pluginapi.ChatChunk{Content: rest}
			}
			flushDone()
			return
		}
		if message, failed := streamDataError(data); failed {
			// Before any of the answer was sent, it's asked for again; only
			// its thinking went out (#755).
			if again != nil && formatMismatch(message) && content.Len() == 0 {
				resp.Body.Close()
				pipe(ch, again, message)
				return
			}
			ch <- pluginapi.ChatChunk{Error: message, Done: true}
			return
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
					// What a reasoning model thinks first (#701).
					Reasoning string `json:"reasoning_content"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage   *usagePayload   `json:"usage"`
			Timings *timingsPayload `json:"timings"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		if chunk.Timings != nil {
			timings = chunk.Timings
		}
		if len(chunk.Choices) == 0 {
			// Some llama.cpp builds emit a final timings-only frame.
			continue
		}
		delta := chunk.Choices[0].Delta.Content
		done := chunk.Choices[0].FinishReason != nil
		if done {
			finish = *chunk.Choices[0].FinishReason
		}
		if thought := chunk.Choices[0].Delta.Reasoning; thought != "" {
			thoughtTokens++
			ch <- pluginapi.ChatChunk{Reasoning: thought}
		}
		if delta = lead.feed(delta); delta != "" {
			if firstToken.IsZero() {
				firstToken = time.Now()
			}
			content.WriteString(delta)
			ch <- pluginapi.ChatChunk{Content: delta}
		}
		if done {
			if rest := lead.flush(); rest != "" {
				content.WriteString(rest)
				ch <- pluginapi.ChatChunk{Content: rest}
			}
			flushDone()
			return
		}
	}
	if err := scanner.Err(); err != nil {
		ch <- pluginapi.ChatChunk{Error: err.Error(), Done: true}
		return
	}
	flushDone()
}

type usagePayload struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type timingsPayload struct {
	PromptN            int     `json:"prompt_n"`
	PromptMS           float64 `json:"prompt_ms"`
	PromptPerSecond    float64 `json:"prompt_per_second"`
	PredictedN         int     `json:"predicted_n"`
	PredictedMS        float64 `json:"predicted_ms"`
	PredictedPerSecond float64 `json:"predicted_per_second"`
	// CacheN is how many prompt tokens came from the cache.
	CacheN int `json:"cache_n"`
	// DraftN is how many tokens speculation guessed, and DraftNAccepted how
	// many the model kept (#835).
	DraftN         int `json:"draft_n"`
	DraftNAccepted int `json:"draft_n_accepted"`
}

func mergeServerMetrics(
	usage *usagePayload,
	timings *timingsPayload,
	started, firstToken, ended time.Time,
	content string,
) *pluginapi.GenerationMetrics {
	m := &pluginapi.GenerationMetrics{}
	totalMs := ended.Sub(started).Seconds() * 1000
	m.TotalMs = totalMs
	if !firstToken.IsZero() {
		m.TTFTMs = firstToken.Sub(started).Seconds() * 1000
		m.EvalMs = ended.Sub(firstToken).Seconds() * 1000
	} else {
		m.EvalMs = totalMs
	}

	if timings != nil {
		// prompt_n counts only the tokens processed for this call; the rest
		// of the prompt came from llama-server's cache. The prompt's size,
		// which is what fills the window, is both (§66).
		if timings.PromptN+timings.CacheN > 0 {
			m.PromptTokens = timings.PromptN + timings.CacheN
		}
		if timings.PromptPerSecond == 0 && timings.PromptMS > 0 && timings.PromptN > 0 {
			m.PromptTokPerSec = float64(timings.PromptN) / (timings.PromptMS / 1000)
		}
		if timings.PredictedN > 0 {
			m.CompletionTokens = timings.PredictedN
		}
		if timings.PromptMS > 0 {
			m.PromptMs = timings.PromptMS
		}
		if timings.PredictedMS > 0 {
			m.EvalMs = timings.PredictedMS
		}
		if timings.PromptPerSecond > 0 {
			m.PromptTokPerSec = timings.PromptPerSecond
		}
		if timings.PredictedPerSecond > 0 {
			m.EvalTokPerSec = timings.PredictedPerSecond
		}
		m.CachedTokens = timings.CacheN
		m.DraftTokens, m.DraftAccepted = timings.DraftN, timings.DraftNAccepted
	}
	if usage != nil {
		if usage.PromptTokens > 0 {
			m.PromptTokens = usage.PromptTokens
		}
		if usage.CompletionTokens > 0 {
			m.CompletionTokens = usage.CompletionTokens
		}
		if usage.TotalTokens > 0 {
			m.TotalTokens = usage.TotalTokens
		}
	}
	if m.CompletionTokens == 0 && content != "" {
		m.CompletionTokens = estimateTokens(content)
	}
	if m.TotalTokens == 0 {
		m.TotalTokens = m.PromptTokens + m.CompletionTokens
	}
	if m.PromptMs == 0 && m.TTFTMs > 0 {
		m.PromptMs = m.TTFTMs
	}
	if m.PromptTokPerSec == 0 && m.PromptMs > 0 && m.PromptTokens > 0 {
		m.PromptTokPerSec = float64(m.PromptTokens) / (m.PromptMs / 1000)
	}
	if m.EvalTokPerSec == 0 && m.EvalMs > 0 && m.CompletionTokens > 0 {
		m.EvalTokPerSec = float64(m.CompletionTokens) / (m.EvalMs / 1000)
	}
	return m
}

func estimateTokens(s string) int {
	n := len(strings.Fields(s))
	if n == 0 && s != "" {
		return max(1, len(s)/4)
	}
	return n
}

// streamDataError reads a llama-server failure frame. A token frame has no error object.
func streamDataError(data string) (string, bool) {
	var payload struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal([]byte(data), &payload) != nil || len(payload.Error) == 0 || string(payload.Error) == "null" {
		return "", false
	}
	var asString string
	if json.Unmarshal(payload.Error, &asString) == nil {
		asString = strings.TrimSpace(asString)
		if asString != "" {
			return asString, true
		}
	}
	var asObject struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(payload.Error, &asObject) == nil {
		message := strings.TrimSpace(asObject.Message)
		if message != "" {
			return message, true
		}
	}
	return "", false
}

// reasoningTokens counts a reasoning model's thinking with the model's own
// tokenizer, at the llama-server that answered; an estimate when it can't.
func reasoningTokens(resp *http.Response, thought string) int {
	if resp.Request != nil && resp.Request.URL != nil {
		endpoint := resp.Request.URL.Scheme + "://" + resp.Request.URL.Host
		ctx, cancel := context.WithTimeout(context.WithoutCancel(resp.Request.Context()), 5*time.Second)
		defer cancel()
		if n, err := Tokenize(ctx, endpoint, thought); err == nil {
			return n
		}
	}
	return estimateTokens(thought)
}

// thinkEnd is how a reasoning model ends its thinking.
const thinkEnd = "</think>"

// dropClosedThink is an answer without the </think> it can start with when
// thinking is off: llama.cpp ends the skipped thinking with it.
func dropClosedThink(content string) string {
	trimmed := strings.TrimLeft(content, " \t\r\n")
	if strings.HasPrefix(trimmed, thinkEnd) {
		return strings.TrimLeft(trimmed[len(thinkEnd):], " \t\r\n")
	}
	return content
}

// closedThink drops that </think> from a streamed answer: the answer's
// start is held back until it's clearly the tag or clearly not.
type closedThink struct {
	pending string
	done    bool
}

// feed is what to send of delta now.
func (l *closedThink) feed(delta string) string {
	if l.done {
		return delta
	}
	l.pending += delta
	trimmed := strings.TrimLeft(l.pending, " \t\r\n")
	switch {
	case trimmed == "", strings.HasPrefix(thinkEnd, trimmed):
		// Only space so far, or the start of the tag: wait.
		return ""
	case strings.HasPrefix(trimmed, thinkEnd):
		rest := strings.TrimLeft(trimmed[len(thinkEnd):], " \t\r\n")
		if rest == "" {
			return ""
		}
		l.done = true
		return rest
	}
	l.done = true
	return l.pending
}

// flush is whatever is still held back when the answer ends.
func (l *closedThink) flush() string {
	if l.done {
		return ""
	}
	l.done = true
	return dropClosedThink(l.pending)
}
