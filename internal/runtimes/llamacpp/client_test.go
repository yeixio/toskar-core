package llamacpp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

func TestMergeServerMetricsFromTimings(t *testing.T) {
	started := time.Now().Add(-200 * time.Millisecond)
	first := started.Add(40 * time.Millisecond)
	ended := started.Add(180 * time.Millisecond)
	m := mergeServerMetrics(
		nil,
		&timingsPayload{
			PromptN:            36,
			PromptMS:           36.7,
			PromptPerSecond:    980,
			PredictedN:         12,
			PredictedMS:        70,
			PredictedPerSecond: 171.4,
		},
		started, first, ended, "hello there world",
	)
	if m.PromptTokens != 36 || m.CompletionTokens != 12 {
		t.Fatalf("tokens: %+v", m)
	}
	if m.EvalTokPerSec < 170 || m.EvalTokPerSec > 172 {
		t.Fatalf("eval tok/s: %v", m.EvalTokPerSec)
	}
	if m.PromptMs != 36.7 || m.EvalMs != 70 {
		t.Fatalf("ms: %+v", m)
	}
}

func TestPromptTokensIncludeCachedTokens(t *testing.T) {
	// A follow-up turn: llama-server reused 44 tokens and processed 1.
	started := time.Now().Add(-100 * time.Millisecond)
	m := mergeServerMetrics(nil, &timingsPayload{PromptN: 1, CacheN: 44, PromptMS: 5, PredictedN: 7},
		started, started.Add(10*time.Millisecond), time.Now(), "hi")
	if m.PromptTokens != 45 || m.CachedTokens != 44 {
		t.Fatalf("prompt %d cached %d", m.PromptTokens, m.CachedTokens)
	}
	// Speed is for the tokens actually processed.
	if m.PromptTokPerSec != 200 {
		t.Fatalf("prompt tok/s %v", m.PromptTokPerSec)
	}
}

func TestMergeServerMetricsFallbackEstimate(t *testing.T) {
	started := time.Now().Add(-100 * time.Millisecond)
	ended := time.Now()
	m := mergeServerMetrics(nil, nil, started, time.Time{}, ended, "one two three four")
	if m.CompletionTokens != 4 {
		t.Fatalf("expected estimated 4 tokens, got %d", m.CompletionTokens)
	}
	if m.TotalMs <= 0 {
		t.Fatal("expected total ms")
	}
}

func TestStreamDataError(t *testing.T) {
	msg, ok := streamDataError(`{"error":{"message":"Compute error.","type":"server_error","code":500}}`)
	if !ok || msg != "Compute error." {
		t.Fatalf("message %q ok=%v", msg, ok)
	}
	if _, ok := streamDataError(`{"choices":[{"delta":{"content":"Hi"}}]}`); ok {
		t.Fatal("a token frame is not an error")
	}
}

func TestContextWindowUsesTheRunningLimit(t *testing.T) {
	if ContextWindow(0) != 8192 || ContextWindow(131072) != 8192 || ContextWindow(4096) != 4096 {
		t.Fatalf("window(0)=%d window(128k)=%d window(4k)=%d", ContextWindow(0), ContextWindow(131072), ContextWindow(4096))
	}
}

// llama-server sends a reasoning model's thinking as reasoning_content
// (this is its real output for DeepSeek R1 Distill 7B); it comes through as
// Reasoning, never as the answer (#701).
func TestReasoningContentIsKeptApart(t *testing.T) {
	frames := strings.Join([]string{
		`data: {"choices":[{"finish_reason":null,"index":0,"delta":{"reasoning_content":"\nTo"}}]}`,
		`data: {"choices":[{"finish_reason":null,"index":0,"delta":{"reasoning_content":" solve"}}]}`,
		`data: {"choices":[{"finish_reason":null,"index":0,"delta":{"content":"391"}}]}`,
		`data: {"choices":[{"finish_reason":"stop","index":0,"delta":{}}]}`,
		`data: [DONE]`,
	}, "\n\n") + "\n\n"
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(frames))}
	ch := make(chan pluginapi.ChatChunk, 16)
	go NewClient().readStream(resp, ch, time.Now(), nil, false)
	var content, thinking strings.Builder
	for c := range ch {
		content.WriteString(c.Content)
		thinking.WriteString(c.Reasoning)
	}
	if content.String() != "391" || thinking.String() != "\nTo solve" {
		t.Fatalf("content %q thinking %q", content.String(), thinking.String())
	}

	body := `{"choices":[{"message":{"content":"391","reasoning_content":"17 times 23"}}]}`
	ch = make(chan pluginapi.ChatChunk, 4)
	go NewClient().readNonStream(&http.Response{Body: io.NopCloser(strings.NewReader(body))}, ch, time.Now(), nil, false)
	content.Reset()
	thinking.Reset()
	for c := range ch {
		content.WriteString(c.Content)
		thinking.WriteString(c.Reasoning)
	}
	if content.String() != "391" || thinking.String() != "17 times 23" {
		t.Fatalf("non-stream content %q thinking %q", content.String(), thinking.String())
	}
}

// A reply llama.cpp can't read, as when Muse Glimmer calls places.search in
// a turn with no tools, is asked for once more with a note in the person's
// message, and that answer comes back in its place (#755): streamed, where
// the error follows the thinking, and as a 500 before anything.
func TestChatAsksAgainAfterAnUnreadableReply(t *testing.T) {
	const mismatch = `{"error":{"code":500,"message":"The model produced output that does not match the expected peg-native format","type":"server_error"}}`
	for _, stream := range []bool{true, false} {
		var bodies []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			bodies = append(bodies, string(b))
			if len(bodies) == 1 {
				if stream {
					fmt.Fprint(w, `data: {"choices":[{"delta":{"reasoning_content":"Let's search."}}]}`+"\n\n")
					fmt.Fprint(w, "data: "+mismatch+"\n\n")
					return
				}
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, mismatch)
				return
			}
			if stream {
				fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"Try Heritage Coffee."},"finish_reason":"stop"}]}`+"\n\n")
				return
			}
			fmt.Fprint(w, `{"choices":[{"message":{"content":"Try Heritage Coffee."}}]}`)
		}))
		ch, err := NewClient().Chat(context.Background(), pluginapi.ChatRequest{
			ModelEndpoint: srv.URL, Stream: stream,
			Messages: []pluginapi.ChatMessage{{Role: "system", Content: "Be brief."}, {Role: "user", Content: "Find a cafe in downtown Juneau"}},
		})
		if err != nil {
			t.Fatalf("stream=%v: %v", stream, err)
		}
		var answer, failed string
		for c := range ch {
			answer += c.Content
			if c.Error != "" {
				failed = c.Error
			}
		}
		srv.Close()
		if failed != "" || answer != "Try Heritage Coffee." {
			t.Errorf("stream=%v: answer %q, error %q", stream, answer, failed)
		}
		if len(bodies) != 2 || !strings.Contains(bodies[1], "No tools are available for this message") || strings.Contains(bodies[0], "No tools") {
			t.Errorf("stream=%v: asked %d times; second %q", stream, len(bodies), bodies[len(bodies)-1])
		}
	}
	// It's asked again once, not again and again.
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, mismatch)
	}))
	defer srv.Close()
	if _, err := NewClient().Chat(context.Background(), pluginapi.ChatRequest{ModelEndpoint: srv.URL, Messages: []pluginapi.ChatMessage{{Role: "user", Content: "hi"}}}); err == nil || calls != 2 {
		t.Errorf("err %v after %d calls", err, calls)
	}
}

// With tools offered, the note names them.
func TestToolNoteNamesTheOfferedTools(t *testing.T) {
	req := withToolNote(pluginapi.ChatRequest{
		Messages: []pluginapi.ChatMessage{{Role: "user", Content: "Weather in Juneau?"}, {Role: "assistant", Content: "…"}},
		Tools:    []map[string]any{{"type": "function", "function": map[string]any{"name": "web_search"}}},
	})
	if got := req.Messages[0].Content; !strings.Contains(got, "Only these tools are available for this message: web_search.") {
		t.Errorf("note: %q", got)
	}
}

// Why the model stopped, and how long it thought, reach the metrics (#832):
// length when it reached its token limit; reasoning tokens counted from the
// stream's chunks, or with /tokenize for a whole answer.
func TestFinishReasonAndReasoningTokens(t *testing.T) {
	for _, stream := range []bool{true, false} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/tokenize" {
				fmt.Fprint(w, `{"tokens":[1,2,3,4]}`)
				return
			}
			if stream {
				fmt.Fprint(w, `data: {"choices":[{"delta":{"reasoning_content":"Think"}}]}`+"\n\n")
				fmt.Fprint(w, `data: {"choices":[{"delta":{"reasoning_content":"ing."}}]}`+"\n\n")
				fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"Cut"},"finish_reason":"length"}]}`+"\n\n")
				return
			}
			fmt.Fprint(w, `{"choices":[{"message":{"content":"Cut","reasoning_content":"Thinking hard."},"finish_reason":"length"}]}`)
		}))
		ch, err := NewClient().Chat(context.Background(), pluginapi.ChatRequest{ModelEndpoint: srv.URL, Stream: stream, Messages: []pluginapi.ChatMessage{{Role: "user", Content: "hi"}}})
		if err != nil {
			t.Fatal(err)
		}
		var m *pluginapi.GenerationMetrics
		for c := range ch {
			if c.Metrics != nil {
				m = c.Metrics
			}
		}
		srv.Close()
		want := 4 // from /tokenize
		if stream {
			want = 2 // the stream's reasoning chunks
		}
		if m == nil || m.FinishReason != "length" || m.ReasoningTokens != want {
			t.Errorf("stream=%v: metrics %+v", stream, m)
		}
	}
}

// Thinking off (#840): the template's switch and a thinking budget of none
// are sent, and the </think> llama.cpp ends the skipped thinking with is
// dropped from the answer, whole or streamed in any pieces.
func TestNoThinking(t *testing.T) {
	for _, stream := range []bool{false, true} {
		var sent map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&sent)
			if stream {
				for _, piece := range []string{"\n", "\n</th", "ink>", "\n\n", "17 × 23", " = 391"} {
					fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", piece)
				}
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
				return
			}
			fmt.Fprint(w, `{"choices":[{"message":{"content":"\n\n</think>\n\n17 × 23 = 391"},"finish_reason":"stop"}]}`)
		}))
		ch, err := NewClient().Chat(context.Background(), pluginapi.ChatRequest{ModelEndpoint: srv.URL, Stream: stream, NoThinking: true, Messages: []pluginapi.ChatMessage{{Role: "user", Content: "17×23?"}}})
		if err != nil {
			t.Fatal(err)
		}
		content := ""
		for c := range ch {
			content += c.Content
		}
		srv.Close()
		if content != "17 × 23 = 391" {
			t.Errorf("stream=%v: content %q", stream, content)
		}
		kwargs, _ := sent["chat_template_kwargs"].(map[string]any)
		if kwargs["enable_thinking"] != false || sent["thinking_budget_tokens"] != float64(0) {
			t.Errorf("stream=%v: sent %v", stream, sent)
		}
	}
	// Thinking on sends neither, and keeps the answer as it came.
	var sent map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&sent)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"Hi"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()
	ch, _ := NewClient().Chat(context.Background(), pluginapi.ChatRequest{ModelEndpoint: srv.URL, Messages: []pluginapi.ChatMessage{{Role: "user", Content: "hi"}}})
	for range ch {
	}
	if _, ok := sent["chat_template_kwargs"]; ok {
		t.Errorf("sent %v", sent)
	}
}

func TestClosedThink(t *testing.T) {
	for _, c := range []struct {
		pieces []string
		want   string
	}{
		{[]string{"</think>", "Hi"}, "Hi"},
		{[]string{"\n\n</", "think>\n", "\nHi"}, "Hi"},
		{[]string{"Hi", " there"}, "Hi there"},
		{[]string{"<", "b>bold</b>"}, "<b>bold</b>"},
		{[]string{"\n", "Answer"}, "\nAnswer"},
		{[]string{"</think>"}, ""},
		{[]string{"  ", "</thi"}, "  </thi"},
	} {
		l := &closedThink{}
		got := ""
		for _, p := range c.pieces {
			got += l.feed(p)
		}
		got += l.flush()
		if got != c.want {
			t.Errorf("%q: %q, want %q", c.pieces, got, c.want)
		}
	}
}
