package openai

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// A live answer that takes text back still reaches a client as text that
// only grows (#1450).
func TestGrowingText(t *testing.T) {
	for _, c := range []struct {
		name   string
		chunks []pluginapi.ChatChunk
		want   string
	}{
		{"written as it comes", []pluginapi.ChatChunk{{Content: "Juneau "}, {Content: "gets rain."}}, "Juneau gets rain."},
		{"narration taken back for a tool stays, and the answer follows", []pluginapi.ChatChunk{
			{Content: "Let me check."}, {Replace: true}, {Content: "It's "}, {Content: "clean."},
		}, "Let me check.\n\nIt's clean."},
		{"a retry that says the same goes on from there", []pluginapi.ChatChunk{
			{Content: "It's "}, {Replace: true}, {Content: "It's "}, {Content: "clean."},
		}, "It's clean."},
		{"an answer that grows when it settles", []pluginapi.ChatChunk{{Content: "It's"}, {Replace: true, Content: "It's clean."}}, "It's clean."},
		{"a corrected answer comes after the first", []pluginapi.ChatChunk{
			{Content: "Off the topic."}, {Replace: true, Content: "Back on it."},
		}, "Off the topic.\n\nBack on it."},
		{"taken back before anything was sent", []pluginapi.ChatChunk{{Replace: true}, {Content: "Clean."}}, "Clean."},
	} {
		t.Run(c.name, func(t *testing.T) {
			var g growingText
			var got strings.Builder
			for _, chunk := range c.chunks {
				got.WriteString(g.next(chunk))
			}
			if got.String() != c.want {
				t.Fatalf("got %q, want %q", got.String(), c.want)
			}
		})
	}
}

// auto and profiles stream the answer as it's written when asked to,
// and a whole answer is its last version (#1450).
func TestAutoStreamsLive(t *testing.T) {
	h := testHandler(t)
	chat := &scriptChat{chunks: []pluginapi.ChatChunk{{Content: "Let me check."}, {Replace: true}, {Content: "Clean."}, {Done: true}}}
	h.Chat = chat
	var live bool
	call := func(stream bool) string {
		body := `{"model":"auto","messages":[{"role":"user","content":"Is it clean?"}]`
		if stream {
			body += `,"stream":true`
		}
		rec := httptest.NewRecorder()
		h.HandleChatCompletions(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body+`}`)))
		if rec.Code != http.StatusOK {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		live = chat.live
		return rec.Body.String()
	}
	streamed := call(true)
	if !live || !strings.Contains(streamed, `"content":"Let me check."`) || !strings.Contains(streamed, `"content":"\n\nClean."`) {
		t.Fatalf("live %v, streamed %s", live, streamed)
	}
	whole := call(false)
	if live || !strings.Contains(whole, `"content":"Clean."`) || strings.Contains(whole, "Let me check") {
		t.Fatalf("live %v, whole %s", live, whole)
	}
}
