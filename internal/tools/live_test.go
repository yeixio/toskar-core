package tools

import (
	"strings"
	"testing"
)

// The output kinds #842 reads at every split, and what can leak from them.
var liveOutputs = []string{
	"Juneau gets about 230 rainy days a year. ☔ Bring a raincoat.",
	"Let me check.\n" + `{"tool_call":{"id":"internet.search","args":{"query":"Juneau weather"}}}`,
	`<tool_call>{"name":"internet.search","arguments":{"query":"Juneau ferry"}}</tool_call>`,
	`{"tool_call":{"id":"internet.search","args":{"query":"Juneau weather"}}} Here it is.`,
	"```json\n" + `{"tool_call":{"id":"git.status","args":{}}}` + "\n```",
	"It's cloudy.\n" + `{"tool_result":{"tool_id":"internet.search","ok":false,"error":"stack trace line"}}` + "\n<|im_end|>",
	`function_call(internet.search, {"query":"x"})`,
	"Before <|im_end|> after",
	"A sample:\n```json\n{\"a\": 1}\n```\nThat's it.",
	"```go\nfunc main() {\n}\n```\nDone.",
	"Use <b>bold</b>, a < b, and the function_call field.",
	"A set {like this} and an open brace {",
	"\n\n  Leading space, then\n\n\n\nmany blank lines.  \nAnd a trailing one.",
	`files.search {"query":"x"}`,
	"See example.com for more.",
}

// Wrappers and the start of each. No output above may show these unless its
// finished answer does.
var liveLeaks = []string{"tool_call", "tool_result", "<tool", "<|", "function_call(", "```json\n{\"tool", `"tool`, "files.search"}

func feedLive(t *testing.T, output string, pieces []string) {
	t.Helper()
	want := ParseModelOutput(output).Text
	var live LiveText
	var written strings.Builder
	for _, piece := range pieces {
		written.WriteString(live.Feed(piece))
		if written.String() != live.Shown() {
			t.Fatalf("%q: deltas %q, shown %q", output, written.String(), live.Shown())
		}
		for _, leak := range liveLeaks {
			if strings.Contains(live.Shown(), leak) && !strings.Contains(want, leak) {
				t.Fatalf("%q: showed %q at %q", output, leak, live.Shown())
			}
		}
	}
	// What's shown is the start of the answer, except for a loose call,
	// which only the turn can recognize. The rest comes when the turn ends.
	if !strings.HasPrefix(want, live.Shown()) {
		t.Fatalf("%q: showed %q, answer is %q", output, live.Shown(), want)
	}
}

func TestLiveTextEverySplit(t *testing.T) {
	for _, output := range liveOutputs {
		feedLive(t, output, []string{output})
		for i := 1; i < len(output); i++ {
			feedLive(t, output, []string{output[:i], output[i:]})
		}
		var runes []string
		for _, r := range output {
			runes = append(runes, string(r))
		}
		feedLive(t, output, runes)
	}
}

func TestLiveTextShowsAsItGoes(t *testing.T) {
	var live LiveText
	var at []string
	for _, piece := range []string{"Juneau ", "gets ", "rain."} {
		at = append(at, live.Feed(piece))
	}
	if strings.Join(at, "|") != "Juneau| gets| rain." {
		t.Fatalf("deltas: %q", at)
	}
}

// Narration before a call does show, and the turn takes it back.
func TestLiveTextNarrationShowsUntilTheCall(t *testing.T) {
	var live LiveText
	live.Feed("Let me check.\n")
	if live.Shown() != "Let me check." {
		t.Fatalf("shown %q", live.Shown())
	}
	live.Feed(`{"tool_call":{"id":"git.status","args":{}}}`)
	if strings.Contains(live.Shown(), "tool_call") {
		t.Fatalf("shown %q", live.Shown())
	}
}
