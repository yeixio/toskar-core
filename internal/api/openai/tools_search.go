package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"unicode"

	"github.com/yeixio/toskar-core/internal/contextusage"
)

// Deferred tools (#843): an agent with many tools, such as Claude Code with
// MCP servers, sends more tool definitions than a local model's window holds.
// Past a share of the window, the model gets only the tools most likely to
// be wanted, and tools_search to find the rest. Toskar answers tools_search
// itself, adds what it found, and asks the model again, so the agent only
// ever sees the answer, with calls to its own tools.

const (
	// rawToolRoom is the most of a raw request's tools counted in sizing
	// its window; past what the window's share holds, they're deferred.
	rawToolRoom = 8192
	// toolShare is the part of the window a request's tools may take
	// before some are deferred: 1/toolShare.
	toolShare = 4
	// searchRounds is how many times the model may search for tools in
	// one request before its answer is taken as it is.
	searchRounds = 3
	// searchResults is how many tools one search adds.
	searchResults = 5
	// searchToolName is the tool the model searches with; OpenAI's tool
	// names allow no dots.
	searchToolName = "tools_search"
)

var searchTool = json.RawMessage(`{"type":"function","function":{"name":"` + searchToolName + `","description":"Only some of your tools are listed here. To find another, call this with a few words saying what it should do, such as \"search the web\" or \"create a calendar event\"; the tools that match become available, and you can call them next.","parameters":{"type":"object","properties":{"query":{"type":"string","description":"What the tool should do"}},"required":["query"]}}}`)

// chatTool is one Chat Completions tool definition, kept as it came.
type chatTool struct {
	raw         json.RawMessage
	name, about string
	tokens      int
}

func readTools(raw []json.RawMessage) []chatTool {
	out := make([]chatTool, 0, len(raw))
	for _, t := range raw {
		var def struct {
			Function struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"function"`
		}
		_ = json.Unmarshal(t, &def)
		out = append(out, chatTool{raw: t, name: def.Function.Name, about: def.Function.Name + " " + def.Function.Description, tokens: contextusage.Estimate(string(t))})
	}
	return out
}

// toolTokens is about how many tokens a Chat Completions body's tools take.
func toolTokens(body []byte) int {
	var b struct {
		Tools json.RawMessage `json:"tools"`
	}
	if json.Unmarshal(body, &b) != nil {
		return 0
	}
	return contextusage.Estimate(string(b.Tools))
}

// words are text's lowercase words of three letters or more, for matching
// a tool with what's asked.
func words(text string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) {
		if len(w) >= 3 {
			out[w] = true
		}
	}
	return out
}

// score is how many of query's words a tool's name and description share.
func score(t chatTool, query map[string]bool) int {
	n := 0
	for w := range words(t.about) {
		if query[w] {
			n++
		}
	}
	return n
}

// chooseTools splits tools into those the model gets, within budget tokens,
// and the deferred rest. Tools the conversation already called come first,
// then those sharing the most words with the latest request, then the
// agent's own order, which puts its built-in tools first.
func chooseTools(tools []chatTool, used map[string]bool, request string, budget int) (kept, deferred []chatTool) {
	query := words(request)
	order := make([]int, len(tools))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		ta, tb := tools[order[a]], tools[order[b]]
		if used[ta.name] != used[tb.name] {
			return used[ta.name]
		}
		return score(ta, query) > score(tb, query)
	})
	spent := contextusage.Estimate(string(searchTool))
	keep := map[int]bool{}
	for _, i := range order {
		if spent+tools[i].tokens <= budget || used[tools[i].name] {
			keep[i] = true
			spent += tools[i].tokens
		}
	}
	for i, t := range tools {
		if keep[i] {
			kept = append(kept, t)
		} else {
			deferred = append(deferred, t)
		}
	}
	return kept, deferred
}

// search is the deferred tools that best match query, best first.
func search(deferred []chatTool, query string) []chatTool {
	q := words(query)
	type hit struct {
		t chatTool
		n int
	}
	var hits []hit
	for _, t := range deferred {
		if n := score(t, q); n > 0 {
			hits = append(hits, hit{t, n})
		}
	}
	sort.SliceStable(hits, func(a, b int) bool { return hits[a].n > hits[b].n })
	var out []chatTool
	for i := 0; i < len(hits) && i < searchResults; i++ {
		out = append(out, hits[i].t)
	}
	return out
}

// rawSend sends a raw request's Chat Completions body to the model. Most go
// as they are. One whose tools take more than its share of the model's
// window gets its tools deferred: the model answers without streaming,
// searching for tools as it needs them, and the answer is given back as
// llama-server would have given it, streamed or not.
func rawSend(ctx context.Context, endpoint string, body []byte, window int) (*http.Response, error) {
	if window <= 0 || toolTokens(body) <= window/toolShare {
		return rawPost(ctx, endpoint, body)
	}
	var req map[string]json.RawMessage
	if err := json.Unmarshal(body, &req); err != nil {
		return rawPost(ctx, endpoint, body)
	}
	var rawTools []json.RawMessage
	var messages []map[string]any
	if json.Unmarshal(req["tools"], &rawTools) != nil || json.Unmarshal(req["messages"], &messages) != nil {
		return rawPost(ctx, endpoint, body)
	}
	var stream, usage bool
	_ = json.Unmarshal(req["stream"], &stream)
	var opts struct {
		IncludeUsage bool `json:"include_usage"`
	}
	_ = json.Unmarshal(req["stream_options"], &opts)
	usage = opts.IncludeUsage

	kept, deferred := chooseTools(readTools(rawTools), calledTools(messages), lastRequest(messages), window/toolShare)
	req["stream"] = json.RawMessage("false")
	delete(req, "stream_options")
	var answer chatAnswer
	var spentIn, spentOut int
	for round := 0; ; round++ {
		tools := make([]json.RawMessage, 0, len(kept)+1)
		for _, t := range kept {
			tools = append(tools, t.raw)
		}
		if round < searchRounds && len(deferred) > 0 {
			tools = append(tools, searchTool)
		}
		req["tools"], _ = json.Marshal(tools)
		req["messages"], _ = json.Marshal(messages)
		next, _ := json.Marshal(req)
		resp, err := rawPost(ctx, endpoint, next)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode >= 300 {
			return resp, nil
		}
		answer = chatAnswer{}
		err = json.NewDecoder(resp.Body).Decode(&answer)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("the model's answer wasn't readable: %w", err)
		}
		spentIn += answer.Usage.PromptTokens
		spentOut += answer.Usage.CompletionTokens
		if len(answer.Choices) == 0 {
			break
		}
		msg := answer.Choices[0].Message
		var searches, own []toolCall
		for _, c := range msg.ToolCalls {
			if c.Function.Name == searchToolName {
				searches = append(searches, c)
			} else {
				own = append(own, c)
			}
		}
		// An answer, or a call to one of the agent's tools, goes back to
		// the agent, without any search beside it.
		if len(searches) == 0 || len(own) > 0 || round >= searchRounds {
			answer.Choices[0].Message.ToolCalls = own
			if len(own) == 0 && answer.Choices[0].FinishReason == "tool_calls" {
				answer.Choices[0].FinishReason = "stop"
			}
			break
		}
		calls := make([]map[string]any, 0, len(searches))
		var results []map[string]any
		for _, c := range searches {
			var args struct {
				Query string `json:"query"`
			}
			_ = json.Unmarshal([]byte(c.Function.Arguments), &args)
			found := search(deferred, args.Query)
			deferred = without(deferred, found)
			kept = append(kept, found...)
			calls = append(calls, map[string]any{"id": c.ID, "type": "function", "function": map[string]any{"name": searchToolName, "arguments": c.Function.Arguments}})
			results = append(results, map[string]any{"role": "tool", "tool_call_id": c.ID, "content": foundText(found)})
		}
		messages = append(messages, map[string]any{"role": "assistant", "content": msg.Content, "tool_calls": calls})
		for _, r := range results {
			messages = append(messages, r)
		}
	}
	answer.Usage.PromptTokens, answer.Usage.CompletionTokens = spentIn, spentOut
	if stream {
		return synthesizedStream(answer, usage), nil
	}
	raw, _ := json.Marshal(answer.asChat())
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(raw))}, nil
}

// calledTools are the tools the conversation's assistant turns called, so
// a tool the agent already uses stays with the model.
func calledTools(messages []map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, m := range messages {
		calls, _ := m["tool_calls"].([]any)
		for _, c := range calls {
			if call, ok := c.(map[string]any); ok {
				if f, ok := call["function"].(map[string]any); ok {
					if name, ok := f["name"].(string); ok {
						out[name] = true
					}
				}
			}
		}
	}
	return out
}

// lastRequest is the text of the conversation's last user turn.
func lastRequest(messages []map[string]any) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i]["role"] != "user" {
			continue
		}
		switch c := messages[i]["content"].(type) {
		case string:
			return c
		case []any:
			var texts []string
			for _, p := range c {
				if part, ok := p.(map[string]any); ok {
					if t, ok := part["text"].(string); ok {
						texts = append(texts, t)
					}
				}
			}
			return strings.Join(texts, " ")
		}
	}
	return ""
}

func without(tools, drop []chatTool) []chatTool {
	gone := map[string]bool{}
	for _, t := range drop {
		gone[t.name] = true
	}
	var out []chatTool
	for _, t := range tools {
		if !gone[t.name] {
			out = append(out, t)
		}
	}
	return out
}

// foundText is a search's result as the model reads it.
func foundText(found []chatTool) string {
	if len(found) == 0 {
		return "No other tool matches. Answer with the tools you have, or say what's missing."
	}
	var b strings.Builder
	b.WriteString("These tools are available now; call them as you need:\n")
	for _, t := range found {
		fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(t.about))
	}
	return b.String()
}

// asChat is the answer as llama-server's Chat Completions JSON.
func (c chatAnswer) asChat() map[string]any {
	choices := []map[string]any{}
	for _, ch := range c.Choices {
		msg := map[string]any{"role": "assistant", "content": ch.Message.Content}
		if len(ch.Message.ToolCalls) > 0 {
			calls := make([]map[string]any, 0, len(ch.Message.ToolCalls))
			for _, tc := range ch.Message.ToolCalls {
				calls = append(calls, map[string]any{"id": tc.ID, "type": "function", "function": map[string]any{"name": tc.Function.Name, "arguments": tc.Function.Arguments}})
			}
			msg["tool_calls"] = calls
		}
		choices = append(choices, map[string]any{"index": 0, "message": msg, "finish_reason": ch.FinishReason})
	}
	return map[string]any{
		"object": "chat.completion", "model": "toskar", "choices": choices,
		"usage": map[string]int{"prompt_tokens": c.Usage.PromptTokens, "completion_tokens": c.Usage.CompletionTokens, "total_tokens": c.Usage.PromptTokens + c.Usage.CompletionTokens},
	}
}

// synthesizedStream is a whole answer as llama-server's stream would carry
// it: the text, each tool call, the finish, and usage when asked.
func synthesizedStream(c chatAnswer, usage bool) *http.Response {
	var b bytes.Buffer
	chunk := func(v map[string]any) {
		// model is renamed as the request named it, as llama-server's is.
		v["object"], v["model"] = "chat.completion.chunk", "toskar"
		raw, _ := json.Marshal(v)
		fmt.Fprintf(&b, "data: %s\n\n", raw)
	}
	finish := "stop"
	if len(c.Choices) > 0 {
		ch := c.Choices[0]
		finish = ch.FinishReason
		if ch.Message.Content != "" {
			chunk(map[string]any{"choices": []map[string]any{{"index": 0, "delta": map[string]any{"role": "assistant", "content": ch.Message.Content}}}})
		}
		for i, tc := range ch.Message.ToolCalls {
			chunk(map[string]any{"choices": []map[string]any{{"index": 0, "delta": map[string]any{"tool_calls": []map[string]any{{
				"index": i, "id": tc.ID, "type": "function", "function": map[string]any{"name": tc.Function.Name, "arguments": tc.Function.Arguments},
			}}}}}})
		}
	}
	chunk(map[string]any{"choices": []map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": finish}}})
	if usage {
		chunk(map[string]any{"choices": []any{}, "usage": map[string]int{"prompt_tokens": c.Usage.PromptTokens, "completion_tokens": c.Usage.CompletionTokens, "total_tokens": c.Usage.PromptTokens + c.Usage.CompletionTokens}})
	}
	b.WriteString("data: [DONE]\n\n")
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(&b)}
}
