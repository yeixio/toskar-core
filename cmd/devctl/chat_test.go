package main

import (
	"bytes"
	"context"
	"errors"
	"iter"
	"strings"
	"sync"
	"testing"
	"time"

	toskar "github.com/yeixio/toskar-sdks/go"
	"github.com/yeixio/toskar-sdks/go/api"

	"github.com/yeixio/toskar-core/internal/askdata"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// fakeChat is Toskar for chat: each turn answers "Answer N", and a turn
// whose message asks for a tool waits on its approval first.
type fakeChat struct {
	mu        sync.Mutex
	created   []string
	sent      []api.ChatJSONRequestBody
	decisions []string
	events    chan api.Event
	decided   chan struct{}
	convs     []contracts.Conversation
	msgs      map[string][]contracts.Message
}

func newFakeChat() *fakeChat {
	return &fakeChat{events: make(chan api.Event, 4), decided: make(chan struct{}, 4), msgs: map[string][]contracts.Message{}}
}

func (f *fakeChat) createConversation(profile, model string) (contracts.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := "0000000" + string(rune('a'+len(f.created)))
	f.created = append(f.created, profile+"|"+model)
	return contracts.Conversation{ID: id}, nil
}

func (f *fakeChat) conversations() ([]contracts.Conversation, error) { return f.convs, nil }

func (f *fakeChat) messages(id string) ([]contracts.Message, error) { return f.msgs[id], nil }

func (f *fakeChat) stream(ctx context.Context, req api.ChatJSONRequestBody) iter.Seq2[toskar.ChatStreamEvent, error] {
	f.mu.Lock()
	f.sent = append(f.sent, req)
	n := len(f.sent)
	f.mu.Unlock()
	return func(yield func(toskar.ChatStreamEvent, error) bool) {
		if strings.Contains(req.Message, "run ls") {
			if !yield(toskar.ChatStreamEvent{Type: "token", Content: "Let me look. "}, nil) {
				return
			}
			payload := map[string]any{"conversation_id": *req.ConversationId, "request_id": "req1", "tool_id": "shell.run", "reason": "to list the files", "args": map[string]any{"command": "ls"}}
			f.events <- api.Event{Type: "tool.requested", Payload: &payload}
			select {
			case <-f.decided:
			case <-time.After(5 * time.Second):
				yield(toskar.ChatStreamEvent{}, errors.New("no decision"))
				return
			case <-ctx.Done():
				return
			}
		}
		if !yield(toskar.ChatStreamEvent{Type: "token", Content: "Answer " + string(rune('0'+n))}, nil) {
			return
		}
		yield(toskar.ChatStreamEvent{Type: "done", Metrics: &api.GenerationMetrics{EvalTokPerSec: 42}}, nil)
	}
}

func (f *fakeChat) eventsSeq(ctx context.Context) iter.Seq2[api.Event, error] {
	return func(yield func(api.Event, error) bool) {
		for {
			select {
			case e := <-f.events:
				if !yield(e, nil) {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}
}

func (f *fakeChat) decide(requestID string, allow bool) error {
	f.mu.Lock()
	f.decisions = append(f.decisions, requestID+map[bool]string{true: " allow", false: " deny"}[allow])
	f.mu.Unlock()
	f.decided <- struct{}{}
	return nil
}

// eventsFake wraps fakeChat so its events method has the interface's name.
type eventsFake struct{ *fakeChat }

func (e eventsFake) events(ctx context.Context) iter.Seq2[api.Event, error] { return e.eventsSeq(ctx) }

func runChat(t *testing.T, f *fakeChat, args []string, input string, terminal bool) (string, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err := chatCommand(args, chatIO{in: strings.NewReader(input), inTerminal: terminal, out: &out, outTerminal: false, errOut: &errOut},
		func() (chatAPI, error) { return eventsFake{f}, nil })
	return out.String(), errOut.String(), err
}

// A chat in the terminal: answers stream, a tool that asks first waits for
// y, and /model changes the model for the next message (#959).
func TestChatSession(t *testing.T) {
	f := newFakeChat()
	out, errOut, err := runChat(t, f, []string{"--model", "qwen2.5-7b-q4", "hello"}, "please run ls\ny\n/model auto\nthanks\n/exit\n", true)
	if err != nil {
		t.Fatalf("%v\n%s", err, errOut)
	}
	for _, want := range []string{"Answer 1", "Let me look.", "wants to run shell.run, to list the files", `"command": "ls"`, "Allow? [y]es, [n]o:", "Answer 2", "Answer 3"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in:\n%s", want, out)
		}
	}
	if len(f.created) != 1 || f.created[0] != "|qwen2.5-7b-q4" {
		t.Errorf("created %v", f.created)
	}
	if len(f.decisions) != 1 || f.decisions[0] != "req1 allow" {
		t.Errorf("decisions %v", f.decisions)
	}
	if len(f.sent) != 3 || *f.sent[0].ModelId != "qwen2.5-7b-q4" || *f.sent[2].ModelId != "auto" || *f.sent[0].Client != "terminal" || *f.sent[1].ConversationId != "0000000a" {
		t.Errorf("sent %+v", f.sent)
	}
}

// Piped in, chat answers once with the input as data, refuses a tool that
// would need a yes, and says how to continue (#959).
func TestChatPiped(t *testing.T) {
	f := newFakeChat()
	out, errOut, err := runChat(t, f, []string{"Why did this fail? Then run ls"}, "panic: boom\n", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.sent) != 1 || !strings.Contains(f.sent[0].Message, "panic: boom") || !strings.Contains(f.sent[0].Message, "Why did this fail?") {
		t.Fatalf("sent %+v", f.sent)
	}
	if !strings.Contains(out, "Not allowed: there's no one at the terminal") || len(f.decisions) != 1 || f.decisions[0] != "req1 deny" {
		t.Errorf("tool: %v\n%s", f.decisions, out)
	}
	if !strings.Contains(errOut, "toskarctl chat --resume 0000000a") {
		t.Errorf("stderr: %s", errOut)
	}
	if _, _, err := runChat(t, newFakeChat(), nil, "data", false); err == nil || !strings.Contains(err.Error(), "needs a message") {
		t.Errorf("piped without a message: %v", err)
	}
}

// --resume picks from the recent chats, newest first, shows the last
// exchange without the data sent with it, and continues there; --resume <id> goes straight to it (#959).
func TestChatResume(t *testing.T) {
	f := newFakeChat()
	now := time.Now()
	f.convs = []contracts.Conversation{
		{ID: "11111111", Title: "Old", UpdatedAt: now.Add(-time.Hour)},
		{ID: "22222222", Title: "Tires", UpdatedAt: now},
	}
	f.msgs["11111111"] = []contracts.Message{{Role: "user", Content: "first"}, {Role: "user", Content: askdata.Message("Which oil?", "OWNER MANUAL")}, {Role: "assistant", Content: "5W-30."}}
	out, _, err := runChat(t, f, []string{"--resume"}, "2\nand the filter?\n/exit\n", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, " 1. Tires") || !strings.Contains(out, " 2. Old") || !strings.Contains(out, "You: Which oil? (with data)") || strings.Contains(out, "OWNER MANUAL") || !strings.Contains(out, "Toskar: 5W-30.") || strings.Contains(out, "first") {
		t.Errorf("picker and last exchange:\n%s", out)
	}
	if len(f.sent) != 1 || *f.sent[0].ConversationId != "11111111" || len(f.created) != 0 {
		t.Errorf("sent %+v, created %v", f.sent, f.created)
	}

	f = newFakeChat()
	if _, _, err := runChat(t, f, []string{"--resume", "33333333", "hi"}, "/exit\n", true); err != nil || len(f.sent) != 1 || *f.sent[0].ConversationId != "33333333" {
		t.Errorf("--resume id: %v %+v", err, f.sent)
	}
	if got, id := resumeArg([]string{"--resume", "tell me a joke"}); id != "" || len(got) != 2 {
		t.Errorf("a message after --resume isn't an id: %v %q", got, id)
	}
}
