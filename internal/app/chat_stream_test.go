package app

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

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
