package thinking

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/gguf"
	"github.com/yeixio/toskar-core/internal/runtimes/llamacpp"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Thinking per family (#840). Models that think before they answer each
// mark and switch their thinking their own way: Qwen 3's template has
// enable_thinking, DeepSeek R1's distills always open a <think> that
// llama.cpp adds, and gpt-oss thinks in an analysis channel. Toskar turns
// thinking off the same way for all of them (llamacpp.Client, NoThinking),
// and llama.cpp's handling of each changes from release to release. For a
// small model of each family (families.json), on a llama.cpp release, this
// checks that:
//
//   - its file says it thinks (gguf.Inspect), so its answer gets the room
//     and its chat the Thinking setting;
//   - with thinking on, the thinking comes apart from the answer, and the
//     answer is right and carries no thinking tags;
//   - with thinking off, streamed and not, there's no thinking, the answer
//     is right with no tags, and it takes fewer tokens.
//
// It runs where scripts/llama-canary.sh thinking sets it up, and is skipped
// without it.
//
//	TOSKAR_THINKING_LLAMA   directory of the release's llama-server
//	TOSKAR_THINKING_MODELS  directory holding families.json's files
//	TOSKAR_THINKING_REPORT  writes the results as Markdown

// Families is families.json.
type Families struct {
	Question string   `json:"question"`
	Answer   string   `json:"answer"`
	Families []Family `json:"families"`
}

// Family is one family's model.
type Family struct {
	Family string `json:"family"`
	File   string `json:"file"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

func loadFamilies(t *testing.T) Families {
	t.Helper()
	raw, err := os.ReadFile("families.json")
	if err != nil {
		t.Fatal(err)
	}
	var f Families
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

// TestFamiliesFile checks families.json, which every run reads.
func TestFamiliesFile(t *testing.T) {
	f := loadFamilies(t)
	if f.Question == "" || f.Answer == "" || len(f.Families) == 0 {
		t.Fatalf("families.json = %+v", f)
	}
	for _, fam := range f.Families {
		if fam.Family == "" || fam.File == "" || len(fam.SHA256) != 64 || !strings.HasSuffix(fam.URL, "/"+fam.File) || !strings.Contains(fam.URL, "/resolve/") {
			t.Errorf("%+v: needs a family, a file, its URL at a commit, and its SHA-256", fam)
		}
	}
}

// reply is one answer, put together from its chunks.
type reply struct {
	content, reasoning string
	tokens, reasoned   int
	err                string
}

// effortQuestion is one a little longer to think about than the sum, so
// reasoning efforts tell apart.
const effortQuestion = "A train leaves at 9:40 and arrives at 13:15. How long is the trip? Reply with the duration only."

// thinkingTags are what thinking left in an answer would look like.
var thinkingTags = []string{"<think>", "</think>", "<|channel|>", "<|message|>"}

// room is each answer's budget. Qwen 3's 0.6B thinks about the sum for
// 400 to 1,300 tokens on most days, and past 2,048 on some (#1048).
const room = 4096

// tries is how many times a thinking-on answer is asked again when its
// thinking used all of its room and never reached an answer. The check is
// how llama.cpp parts thinking from the answer, not how well a small model
// reasons, and a model that loops on every try still fails.
const tries = 3

func ask(ctx context.Context, endpoint, question string, stream, off bool) reply {
	return askWith(ctx, endpoint, question, stream, off, "")
}

// askWith is ask with a reasoning effort (#1140).
func askWith(ctx context.Context, endpoint, question string, stream, off bool, effort string) reply {
	ch, err := llamacpp.NewClient().Chat(ctx, pluginapi.ChatRequest{
		ModelEndpoint:   endpoint,
		Messages:        []pluginapi.ChatMessage{{Role: "user", Content: question}},
		Stream:          stream,
		NoThinking:      off,
		ReasoningEffort: effort,
		// The temperature is the model's own: thinking models loop when
		// it's 0.
		MaxTokens: room,
	})
	if err != nil {
		return reply{err: err.Error()}
	}
	var r reply
	var content, reasoning strings.Builder
	for chunk := range ch {
		content.WriteString(chunk.Content)
		reasoning.WriteString(chunk.Reasoning)
		if chunk.Error != "" {
			r.err = chunk.Error
		}
		if m := chunk.Metrics; m != nil {
			r.tokens, r.reasoned = m.CompletionTokens, m.ReasoningTokens
		}
	}
	r.content, r.reasoning = strings.TrimSpace(content.String()), strings.TrimSpace(reasoning.String())
	return r
}

// outOfRoom is an answer whose thinking used all of its room.
func (r reply) outOfRoom() bool {
	return r.err == "" && r.content == "" && r.tokens >= room
}

// problems are what's wrong with an answer: thinking where it shouldn't be
// or missing where it should, a wrong answer, or tags in it.
func (r reply) problems(answer string, thinking bool) []string {
	if r.err != "" {
		return []string{"failed: " + r.err}
	}
	var out []string
	if thinking && r.reasoning == "" {
		out = append(out, "no thinking came apart from the answer")
	}
	if !thinking && r.reasoning != "" {
		out = append(out, fmt.Sprintf("thought with thinking off: %.80q", r.reasoning))
	}
	if !strings.Contains(r.content, answer) {
		out = append(out, fmt.Sprintf("answered %.80q, not %s", r.content, answer))
	}
	for _, tag := range thinkingTags {
		if strings.Contains(r.content, tag) {
			out = append(out, "the answer has "+tag)
		}
	}
	return out
}

// TestOutOfRoom checks which answers are asked again: only thinking that
// used all of its room with no answer, never an error or a short answer.
func TestOutOfRoom(t *testing.T) {
	for _, c := range []struct {
		r    reply
		want bool
	}{
		{reply{tokens: room, reasoned: room - 2}, true},
		{reply{tokens: room, content: "42"}, false},
		{reply{tokens: 400}, false},
		{reply{tokens: room, err: "context canceled"}, false},
	} {
		if got := c.r.outOfRoom(); got != c.want {
			t.Errorf("%+v: outOfRoom = %v, want %v", c.r, got, c.want)
		}
	}
}

func TestThinkingFamilies(t *testing.T) {
	llama, models := os.Getenv("TOSKAR_THINKING_LLAMA"), os.Getenv("TOSKAR_THINKING_MODELS")
	if llama == "" || models == "" {
		t.Skip("set by scripts/llama-canary.sh thinking")
	}
	f := loadFamilies(t)
	// The daemon's own way of starting llama-server: its runtime folder
	// holds llamacpp/llama-server.
	dir := t.TempDir()
	if err := os.Symlink(llama, filepath.Join(dir, "llamacpp")); err != nil {
		t.Fatal(err)
	}
	logs := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	rt := llamacpp.New(dir, logs)
	tag := filepath.Base(llama)

	var report strings.Builder
	fmt.Fprintf(&report, "| Family | Thinks | Thinking on | Off, streamed | Off | Problems |\n| --- | --- | --- | --- | --- | --- |\n")
	failed := false
	for _, fam := range f.Families {
		t.Run(fam.Family, func(t *testing.T) {
			var problems []string
			defer func() {
				if len(problems) > 0 {
					failed = true
					for _, p := range problems {
						t.Error(p)
					}
				}
			}()
			path := filepath.Join(models, fam.File)
			d, err := gguf.Inspect(path)
			if err != nil {
				problems = append(problems, err.Error())
				fmt.Fprintf(&report, "| %s | | | | | %v |\n", fam.Family, err)
				return
			}
			if !d.Thinks {
				problems = append(problems, "its file doesn't say it thinks, so its answer gets too little room and its chat no Thinking setting")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			m, err := rt.StartModel(ctx, pluginapi.ModelStartConfig{ModelID: fam.File, ModelPath: path, Context: 8192})
			if err != nil {
				problems = append(problems, "start: "+err.Error())
				fmt.Fprintf(&report, "| %s | %v | | | | start: %v |\n", fam.Family, d.Thinks, err)
				return
			}
			defer func() { _ = rt.StopModel(context.Background(), m.ID) }()

			on := ask(ctx, m.Endpoint, f.Question, true, false)
			asked := 1
			for ; on.outOfRoom() && asked < tries; asked++ {
				t.Logf("%s: thinking on used all %d tokens without answering; asking again", fam.Family, room)
				on = ask(ctx, m.Endpoint, f.Question, true, false)
			}
			offStream := ask(ctx, m.Endpoint, f.Question, true, true)
			off := ask(ctx, m.Endpoint, f.Question, false, true)
			for _, c := range []struct {
				name     string
				r        reply
				thinking bool
			}{{"thinking on", on, true}, {"thinking off, streamed", offStream, false}, {"thinking off", off, false}} {
				for _, p := range c.r.problems(f.Answer, c.thinking) {
					problems = append(problems, c.name+": "+p)
				}
			}
			if on.err == "" && on.reasoned == 0 {
				problems = append(problems, "thinking on: no reasoning tokens counted")
			}
			if d.Harmony {
				// gpt-oss thinks as long as its reasoning effort says, which
				// its chat template takes (#1140): high, at length, and low,
				// briefly. On this Mac, a train's trip took 30 to 93 tokens
				// low and 193 to 743 high.
				low := askWith(ctx, m.Endpoint, effortQuestion, true, false, "low")
				high := askWith(ctx, m.Endpoint, effortQuestion, true, false, "high")
				if low.err == "" && high.err == "" && high.reasoned <= low.reasoned {
					problems = append(problems, fmt.Sprintf("reasoning effort: high thought %d tokens, not more than low's %d", high.reasoned, low.reasoned))
				}
				t.Logf("%s on %s: reasoning effort low %d thinking tokens, high %d", fam.Family, tag, low.reasoned, high.reasoned)
			}
			for _, r := range []reply{offStream, off} {
				if r.err == "" && on.err == "" && r.tokens >= on.tokens {
					problems = append(problems, fmt.Sprintf("thinking off took %d tokens, not fewer than thinking on's %d", r.tokens, on.tokens))
				}
			}
			if on.outOfRoom() {
				problems = append(problems, fmt.Sprintf("thinking on: used all %d tokens without answering, %d times", room, asked))
			}
			onCell := fmt.Sprintf("%d tokens (%d thinking)", on.tokens, on.reasoned)
			if asked > 1 {
				onCell += fmt.Sprintf(", try %d", asked)
			}
			t.Logf("%s on %s: thinks %v; on %d tokens (%d thinking), try %d; off streamed %d, off %d", fam.Family, tag, d.Thinks, on.tokens, on.reasoned, asked, offStream.tokens, off.tokens)
			fmt.Fprintf(&report, "| %s | %v | %s | %d tokens | %d tokens | %s |\n", fam.Family, d.Thinks, onCell, offStream.tokens, off.tokens, strings.Join(problems, "; "))
		})
	}
	if path := os.Getenv("TOSKAR_THINKING_REPORT"); path != "" {
		status := "passed"
		if failed {
			status = "failed"
		}
		out := fmt.Sprintf("### Thinking per family: %s, %s (thinking)\n\n%s", tag, status, report.String())
		if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
			t.Error(err)
		}
	}
}
