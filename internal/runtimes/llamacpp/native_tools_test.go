package llamacpp

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// callText stands in for the caller's protocol.
func callText(name, arguments string) string { return "CALL " + name + " " + arguments }

func streamedWithCalls(t *testing.T, frames string, text func(string, string) string) (content, thinking string) {
	t.Helper()
	ch := make(chan pluginapi.ChatChunk, 64)
	go NewClient().readStream(&http.Response{Body: io.NopCloser(strings.NewReader(frames))}, ch, time.Now(), nil, false, text)
	var c, th strings.Builder
	for chunk := range ch {
		c.WriteString(chunk.Content)
		th.WriteString(chunk.Reasoning)
	}
	return c.String(), th.String()
}

// gpt-oss 20B's reply, recorded from llama-server with the tools offered
// natively: its thinking, then a call streamed in pieces (#1099).
func TestStreamedToolCallBecomesText(t *testing.T) {
	recorded, err := os.ReadFile("testdata/gpt-oss-tool-call.sse")
	if err != nil {
		t.Fatal(err)
	}
	content, thinking := streamedWithCalls(t, string(recorded), callText)
	if content != `CALL terminal {"command":"go test ./..."}` {
		t.Fatalf("content %q", content)
	}
	if thinking == "" {
		t.Fatal("the thinking was lost")
	}
	// Without a way to write it, a call is ignored, as before.
	if content, _ := streamedWithCalls(t, string(recorded), nil); content != "" {
		t.Fatalf("content without callText %q", content)
	}
}

// An answer that has text keeps it: a call is written only into an empty
// answer.
func TestToolCallNeverReplacesText(t *testing.T) {
	frames := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"content":"Done."}}]}`,
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"terminal","arguments":"{}"}}]}}]}`,
		`data: {"choices":[{"finish_reason":"tool_calls","index":0,"delta":{}}]}`,
		`data: [DONE]`,
	}, "\n\n") + "\n\n"
	if content, _ := streamedWithCalls(t, frames, callText); content != "Done." {
		t.Fatalf("content %q", content)
	}
}

func TestNonStreamToolCallBecomesText(t *testing.T) {
	body := `{"choices":[{"finish_reason":"tool_calls","message":{"content":"","reasoning_content":"Read it.","tool_calls":[{"type":"function","function":{"name":"filesystem.read","arguments":"{\"path\":\"tally.go\"}"}}]}}]}`
	ch := make(chan pluginapi.ChatChunk, 4)
	go NewClient().readNonStream(&http.Response{Body: io.NopCloser(strings.NewReader(body))}, ch, time.Now(), nil, false, callText)
	var content strings.Builder
	for chunk := range ch {
		content.WriteString(chunk.Content)
	}
	if content.String() != `CALL filesystem.read {"path":"tally.go"}` {
		t.Fatalf("content %q", content.String())
	}
}
