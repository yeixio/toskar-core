package app

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Every chat error reaches the client with a code (#588): the error's own,
// one recognized from its text, or CHAT_FAILED.
func TestChatStreamErrorsAreCoded(t *testing.T) {
	for _, tc := range []struct {
		name  string
		chunk pluginapi.ChatChunk
		code  string
	}{
		{"its own code", pluginapi.ChatChunk{Error: "Assign a model first.", Code: "NO_MODEL_ASSIGNED", Details: map[string]any{"role": "writer"}}, "NO_MODEL_ASSIGNED"},
		{"recognized", pluginapi.ChatChunk{Error: "llama-server: out of memory"}, "OUT_OF_MEMORY"},
		{"anything else", pluginapi.ChatChunk{Error: "the planner gave up"}, "CHAT_FAILED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ch := make(chan pluginapi.ChatChunk, 2)
			ch <- pluginapi.ChatChunk{Content: "Hel"}
			ch <- tc.chunk
			close(ch)
			rec := httptest.NewRecorder()
			if err := writeChatStream(rec, ch); err != nil {
				t.Fatal(err)
			}
			out := rec.Body.String()
			i := strings.Index(out, "event: error_code\ndata: ")
			if i < 0 || !strings.Contains(out, "event: error\ndata: "+tc.chunk.Error+"\n") {
				t.Fatalf("stream: %q", out)
			}
			line, _, _ := strings.Cut(out[i+len("event: error_code\ndata: "):], "\n")
			var body contracts.ErrorBody
			if err := json.Unmarshal([]byte(line), &body); err != nil || body.Code != tc.code || body.Message != tc.chunk.Error {
				t.Fatalf("error_code %q: %+v %v", line, body, err)
			}
			if tc.chunk.Details != nil && body.Details["role"] != "writer" {
				t.Fatalf("details lost: %+v", body.Details)
			}
		})
	}
}

// A stream that closes without a last chunk, as a stopped run can, still
// ends with done, so clients can treat a stream without done or error as
// cut off (#588).
func TestChatStreamAlwaysEnds(t *testing.T) {
	ch := make(chan pluginapi.ChatChunk, 1)
	ch <- pluginapi.ChatChunk{Content: "partial"}
	close(ch)
	rec := httptest.NewRecorder()
	if err := writeChatStream(rec, ch); err != nil {
		t.Fatal(err)
	}
	if out := rec.Body.String(); !strings.HasSuffix(out, "event: done\ndata: {}\n\n") {
		t.Fatalf("stream: %q", out)
	}
	ch = make(chan pluginapi.ChatChunk, 1)
	ch <- pluginapi.ChatChunk{Done: true}
	close(ch)
	rec = httptest.NewRecorder()
	_ = writeChatStream(rec, ch)
	if n := strings.Count(rec.Body.String(), "event: done"); n != 1 {
		t.Fatalf("done sent %d times: %q", n, rec.Body)
	}
}

// done waits for the channel to close, after the answer is saved, so a
// client that reads the chat on done finds the answer (#707).
func TestChatStreamDoneWaitsForTheEnd(t *testing.T) {
	ch := make(chan pluginapi.ChatChunk)
	rec := httptest.NewRecorder()
	finished := make(chan struct{})
	go func() {
		_ = writeChatStream(rec, ch)
		close(finished)
	}()
	ch <- pluginapi.ChatChunk{Content: "Hi"}
	ch <- pluginapi.ChatChunk{Done: true, Metrics: &pluginapi.GenerationMetrics{CompletionTokens: 3}}
	// The answer is still being saved: the stream has its tokens, not done.
	time.Sleep(20 * time.Millisecond)
	select {
	case <-finished:
		t.Fatal("the stream ended before the channel closed")
	default:
	}
	close(ch)
	<-finished
	out := rec.Body.String()
	if strings.Count(out, "event: done") != 1 || !strings.Contains(out, `"completion_tokens":3`) || !strings.HasSuffix(out, "\n\n") {
		t.Fatalf("stream: %q", out)
	}
}
