package llamacpp

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Model outputs whose markers a split could break (#842): wherever a
// stream is cut, the answer, the thinking, and the tool call come out as
// they do from one piece.
var splitOutputs = []struct {
	name     string
	thinking string
	content  string
}{
	{"plain answer", "", "Juneau gets about 230 rainy days a year. ☔ Bring a raincoat."},
	{"tool call", "", `Let me check.` + "\n" + `{"tool_call":{"id":"internet.search","args":{"query":"Juneau weather"}}}`},
	{"tagged tool call", "", `<tool_call>{"name":"internet.search","arguments":{"query":"Juneau ferry"}}</tool_call>`},
	{"thinking, then an answer", "17 × 23: 17 × 20 is 340, plus 51.", "391"},
	{"thinking, then a tool call", "The person wants today's weather.", `{"tool_call":{"id":"internet.search","args":{"query":"Juneau weather"}}}`},
	{"a mix", "", "It's cloudy.\n" + `{"tool_result":{"tool_id":"internet.search","ok":false,"error":"stack trace line"}}` + "\n<|im_end|>"},
	{"skipped thinking's end", "", "\n</think>\n\nHello from Juneau."},
}

// sse is llama-server's stream for an output, a token a frame, with each
// rune its own token so every marker is split.
func sse(t *testing.T, thinking, content string) string {
	t.Helper()
	var b strings.Builder
	frame := func(delta map[string]string, finish any) {
		data, err := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}})
		if err != nil {
			t.Fatal(err)
		}
		b.WriteString("data: " + string(data) + "\n\n")
	}
	for _, r := range thinking {
		frame(map[string]string{"reasoning_content": string(r)}, nil)
	}
	for _, r := range content {
		frame(map[string]string{"content": string(r)}, nil)
	}
	frame(map[string]string{}, "stop")
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

// pieces reads its parts one Read at a time, as a network might deliver them.
type pieces struct{ parts []string }

func (p *pieces) Read(b []byte) (int, error) {
	for len(p.parts) > 0 && p.parts[0] == "" {
		p.parts = p.parts[1:]
	}
	if len(p.parts) == 0 {
		return 0, io.EOF
	}
	n := copy(b, p.parts[0])
	p.parts[0] = p.parts[0][n:]
	return n, nil
}

type streamed struct {
	content, thinking string
	out               tools.ModelOutput
}

func readSplit(parts []string, noThinking bool) streamed {
	ch := make(chan pluginapi.ChatChunk, 1024)
	go NewClient().readStream(&http.Response{Body: io.NopCloser(&pieces{parts: parts})}, ch, time.Now(), nil, noThinking)
	var content, thinking strings.Builder
	for c := range ch {
		content.WriteString(c.Content)
		thinking.WriteString(c.Reasoning)
	}
	return streamed{content.String(), thinking.String(), tools.ParseModelOutput(content.String())}
}

// Byte splits of the stream, and one byte a read, read as the whole
// stream does: SSE lines cut anywhere still make the same frames.
func TestReadStreamEverySplit(t *testing.T) {
	for _, o := range splitOutputs {
		for _, noThinking := range []bool{false, true} {
			body := sse(t, o.thinking, o.content)
			whole := readSplit([]string{body}, noThinking)
			wantContent := o.content
			if noThinking {
				wantContent = dropClosedThink(o.content)
			}
			if whole.content != wantContent || whole.thinking != o.thinking {
				t.Fatalf("%s (no thinking %v): whole stream gave %q thinking %q", o.name, noThinking, whole.content, whole.thinking)
			}
			if want := tools.ParseModelOutput(wantContent); !reflect.DeepEqual(whole.out, want) {
				t.Fatalf("%s: parsed %+v, want %+v", o.name, whole.out, want)
			}
			check := func(how string, parts []string) {
				if got := readSplit(parts, noThinking); !reflect.DeepEqual(got, whole) {
					t.Fatalf("%s (no thinking %v), %s: got %+v, want %+v", o.name, noThinking, how, got, whole)
				}
			}
			// Every byte of the first frames, then around every line break,
			// where a cut matters to a line reader: every byte would take
			// minutes for the same cases.
			for i := 1; i < len(body); i++ {
				if i > 400 && !strings.Contains(body[i-2:min(i+2, len(body))], "\n") {
					continue
				}
				check("split at byte "+strconv.Itoa(i), []string{body[:i], body[i:]})
			}
			bytes := make([]string, len(body))
			for i := 0; i < len(body); i++ {
				bytes[i] = body[i : i+1]
			}
			check("one byte a read", bytes)
		}
	}
}

// With thinking off, the </think> an answer can start with is dropped
// wherever the deltas split it, and nothing else is.
func TestClosedThinkEverySplit(t *testing.T) {
	for _, s := range []string{
		"</think>Hi",
		"\n\n</think>\n\nHi there",
		"  </thi",
		"</think>",
		"<b>bold</b>",
		"Answer </think> later",
		"\n</thinking> is not the tag",
		"☔ </think>",
	} {
		want := dropClosedThink(s)
		feed := func(how string, parts []string) {
			l := &closedThink{}
			got := ""
			for _, p := range parts {
				got += l.feed(p)
			}
			got += l.flush()
			if got != want {
				t.Errorf("%q, %s: %q, want %q", s, how, got, want)
			}
		}
		feed("whole", []string{s})
		for i := 1; i < len(s); i++ {
			feed("split at byte "+strconv.Itoa(i), []string{s[:i], s[i:]})
		}
		var runes []string
		for _, r := range s {
			runes = append(runes, string(r))
		}
		feed("one rune a delta", runes)
	}
}
