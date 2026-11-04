package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
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
	where     string
	ran       []toolRun
	offered   []contracts.Model
	stopped   []string
	installed []string
	picked    string
	stops     chan struct{}
}

func newFakeChat() *fakeChat {
	return &fakeChat{stops: make(chan struct{}, 4), events: make(chan api.Event, 4), decided: make(chan struct{}, 4), msgs: map[string][]contracts.Message{}}
}

func (f *fakeChat) createConversation(profile, model string) (contracts.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := "0000000" + string(rune('a'+len(f.created)))
	f.created = append(f.created, profile+"|"+model)
	return contracts.Conversation{ID: id}, nil
}

func (f *fakeChat) computer() string { return f.where }

func (f *fakeChat) conversations() ([]contracts.Conversation, error) { return f.convs, nil }

func (f *fakeChat) messages(id string) ([]contracts.Message, error) { return f.msgs[id], nil }

func (f *fakeChat) runs(string) ([]toolRun, error) { return f.ran, nil }

func (f *fakeChat) recommended() string { return f.picked }

func (f *fakeChat) install(id string, out io.Writer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.installed = append(f.installed, id)
	return nil
}

func (f *fakeChat) stop(id string) error {
	f.mu.Lock()
	f.stopped = append(f.stopped, id)
	f.mu.Unlock()
	f.stops <- struct{}{}
	return nil
}

func (f *fakeChat) models() ([]contracts.Model, error) { return f.offered, nil }

func (f *fakeChat) stream(ctx context.Context, req api.ChatJSONRequestBody) iter.Seq2[toskar.ChatStreamEvent, error] {
	f.mu.Lock()
	f.sent = append(f.sent, req)
	n := len(f.sent)
	f.mu.Unlock()
	return func(yield func(toskar.ChatStreamEvent, error) bool) {
		if strings.Contains(req.Message, "change it") {
			for _, e := range []toskar.ChatStreamEvent{
				{Type: "token", Content: "Let me "}, {Type: "token", Content: "check."}, {Type: "replace"},
				{Type: "token", Content: "Clean."}, {Type: "replace", Content: "All clean."}, {Type: "done"},
			} {
				if !yield(e, nil) {
					return
				}
			}
			return
		}
		if strings.Contains(req.Message, "long build") {
			for _, text := range []string{"\x1b[32mcompiling\x1b[0m a\n", "linking", " b\n", "10%\r20%\r30%"} {
				payload := map[string]any{"conversation_id": *req.ConversationId, "tool_id": "terminal", "text": text}
				f.events <- api.Event{Type: "tool.output", Payload: &payload}
			}
			select {
			case <-f.stops:
				yield(toskar.ChatStreamEvent{Type: "token", Content: "Stopped."}, nil)
				yield(toskar.ChatStreamEvent{Type: "done"}, nil)
			case <-time.After(5 * time.Second):
				yield(toskar.ChatStreamEvent{}, errors.New("not stopped"))
			case <-ctx.Done():
			}
			return
		}
		if strings.Contains(req.Message, "wipe it") {
			payload := map[string]any{"conversation_id": *req.ConversationId, "request_id": "req2", "tool_id": "terminal", "reason": "it can delete", "args": map[string]any{"command": "rm -rf build"}, "always_asks": true, "cant_undo": true}
			f.events <- api.Event{Type: "tool.requested", Payload: &payload}
			select {
			case <-f.decided:
			case <-time.After(5 * time.Second):
				yield(toskar.ChatStreamEvent{}, errors.New("no decision"))
				return
			case <-ctx.Done():
				return
			}
			yield(toskar.ChatStreamEvent{Type: "done"}, nil)
			return
		}
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

func (f *fakeChat) decide(requestID string, allow, forChat bool) error {
	f.mu.Lock()
	f.decisions = append(f.decisions, requestID+map[bool]string{true: " allow", false: " deny"}[allow]+map[bool]string{true: " in this chat", false: ""}[forChat])
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

// A chat in the terminal: answers stream, a tool that asks first says
// which computer it runs on and waits for y, and /model changes the model for the next message (#959).
func TestChatSession(t *testing.T) {
	f := newFakeChat()
	f.where = "rig.local"
	out, errOut, err := runChat(t, f, []string{"--model", "qwen2.5-7b-q4", "hello"}, "please run ls\ny\n/model auto\nthanks\n/exit\n", true)
	if err != nil {
		t.Fatalf("%v\n%s", err, errOut)
	}
	for _, want := range []string{"Answer 1", "Let me look.", "wants to run shell.run on rig.local, to list the files", `"command": "ls"`, "Allow? [y]es, [n]o, [a]lways in this chat:", "Answer 2", "Answer 3"} {
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

// The prompt names the Toskar computer only when it's another one (#959).
func TestChatComputer(t *testing.T) {
	for base, want := range map[string]string{"http://127.0.0.1:7331": "", "http://localhost:7331": "", "http://[::1]:7331": "", "https://rig.local:7331": "rig.local", "https://10.0.0.5:7331": "10.0.0.5"} {
		if got := (daemonChat{rest: daemonClient{base: base}}).computer(); got != want {
			t.Errorf("%s: %q, want %q", base, got, want)
		}
	}
}

// a allows the tool for the rest of the chat (#959).
func TestChatAlways(t *testing.T) {
	f := newFakeChat()
	if _, _, err := runChat(t, f, nil, "please run ls\na\n/exit\n", true); err != nil {
		t.Fatal(err)
	}
	if len(f.decisions) != 1 || f.decisions[0] != "req1 allow in this chat" {
		t.Errorf("decisions %v", f.decisions)
	}
}

func TestChatLive(t *testing.T) {
	chat := func(input string, size func() (int, int)) (string, *fakeChat) {
		t.Helper()
		f := newFakeChat()
		var out, errOut bytes.Buffer
		streams := chatIO{in: strings.NewReader(input), inTerminal: true, out: &out, outTerminal: true, errOut: &errOut, size: size}
		if err := chatCommand(nil, streams, func() (chatAPI, error) { return eventsFake{f}, nil }); err != nil {
			t.Fatal(err)
		}
		return out.String(), f
	}
	big := func() (int, int) { return 80, 24 }

	got, f := chat("change it\n/exit\n", big)
	if f.sent[0].Live == nil || !*f.sent[0].Live {
		t.Fatalf("a terminal should ask for a live answer: %+v", f.sent[0])
	}
	want := "Let me check.\r\x1b[JClean.\r\x1b[JAll clean.\n"
	if !strings.Contains(got, want) {
		t.Fatalf("replaces should erase what they replace:\n%q", got)
	}

	// A terminal that can't say its size can't erase: the new text goes below.
	got, _ = chat("change it\n/exit\n", func() (int, int) { return 0, 0 })
	if !strings.Contains(got, "Let me check.\nClean.\nAll clean.\n") || strings.Contains(got, "\x1b[J") {
		t.Fatalf("no size: %q", got)
	}

	// Nor can one where the text no longer fits on the screen.
	got, _ = chat("change it\n/exit\n", func() (int, int) { return 4, 3 })
	if strings.Contains(got, "Let me check.\r") {
		t.Fatalf("erased text that had scrolled away: %q", got)
	}

	// What's above a tool's question stays, and the answer goes below it.
	got, _ = chat("run ls\ny\n/exit\n", big)
	if strings.Contains(got, "\x1b[J") || !strings.Contains(got, "Let me look. \n") {
		t.Fatalf("question: %q", got)
	}
}

func TestChatPipedIsWhole(t *testing.T) {
	f := newFakeChat()
	var out, errOut bytes.Buffer
	streams := chatIO{in: strings.NewReader("data"), out: &out, errOut: &errOut}
	if err := chatCommand([]string{"Why?"}, streams, func() (chatAPI, error) { return eventsFake{f}, nil }); err != nil {
		t.Fatal(err)
	}
	if f.sent[0].Live != nil {
		t.Fatalf("a pipe can't take text back, so it shouldn't ask for live: %+v", f.sent[0])
	}
}

func TestRowsOf(t *testing.T) {
	for _, c := range []struct {
		text string
		cols int
		want int
	}{
		{"", 10, 1},
		{"short", 10, 1},
		{"exactly 10", 10, 1},
		{"eleven cols", 10, 2},
		{"one\ntwo", 10, 2},
		{"one\n", 10, 2},
		{"\u3053\u3093\u306b\u3061\u306f\u4e16\u754c", 10, 2},
		{"e\u0301e\u0301e\u0301e\u0301e\u0301e\u0301e\u0301e\u0301e\u0301e\u0301", 10, 1},
		{"\tx", 8, 2},
	} {
		if got := rowsOf(c.text, c.cols); got != c.want {
			t.Errorf("rowsOf(%q, %d) = %d, want %d", c.text, c.cols, got, c.want)
		}
	}
}

// A command that can't be undone says so, offers no "always", and takes an
// "a" as a no (#1017).
func TestChatDestructiveAlwaysAsks(t *testing.T) {
	f := newFakeChat()
	out, errOut, err := runChat(t, f, nil, "wipe it\na\n/exit\n", true)
	if err != nil {
		t.Fatalf("%v\n%s", err, errOut)
	}
	for _, want := range []string{"rm -rf build", "This can't be undone.", "[n]o (this one asks every time)"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "[a]lways") {
		t.Errorf("offered always:\n%s", out)
	}
	if len(f.decisions) != 1 || !strings.Contains(f.decisions[0], "deny") {
		t.Errorf("decisions %v", f.decisions)
	}
}

// /log says what ran, its result, and who allowed it, oldest first (#1017).
func TestChatLog(t *testing.T) {
	f := newFakeChat()
	out, _, err := runChat(t, f, nil, "/log\n/exit\n", true)
	if err != nil || strings.Contains(out, "you said") {
		t.Fatalf("%v\n%s", err, out)
	}
	f = newFakeChat()
	f.ran = []toolRun{
		{ToolID: "terminal", Status: "denied", Approval: "you", Summary: "rm -rf build", Error: "denied by user"},
		{ToolID: "terminal", Status: "failed", Approval: "session", Summary: "go test ./...", Error: "exit 1"},
		{ToolID: "terminal", Status: "completed", Approval: "you", Summary: "git status"},
	}
	out, _, err = runChat(t, f, nil, "/log\n/exit\n", true)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"git status", "completed, you said yes", "go test ./...", "failed: exit 1, you said always", "rm -rf build", "not run: you said no"}
	at := 0
	for _, w := range want {
		i := strings.Index(out[at:], w)
		if i < 0 {
			t.Fatalf("no %q in order in:\n%s", w, out)
		}
		at += i
	}
}

// Asked for a model that isn't installed, chat says so rather than have another
// answer. One it doesn't list is for Toskar to find, or refuse.
func TestChatModelMustBeInstalled(t *testing.T) {
	f := newFakeChat()
	f.offered = []contracts.Model{{ID: "big"}, {ID: "small", Installed: true}}
	_, _, err := runChat(t, f, []string{"--model", "big", "hello"}, "/exit\n", true)
	var e *exitError
	if !errors.As(err, &e) || e.code != 2 || !strings.Contains(e.msg, "toskarctl models install big") {
		t.Fatalf("err %v", err)
	}
	if len(f.sent) != 0 {
		t.Fatalf("sent %v", f.sent)
	}
	for _, args := range [][]string{{"--model", "small", "hello"}, {"--model", "ext:echo", "hello"}, {"--model", "auto", "hello"}} {
		if _, _, err := runChat(t, newFakeWith(f.offered), args, "/exit\n", true); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	g := newFakeWith(f.offered)
	out, errOut, err := runChat(t, g, []string{"hello"}, "/model big\n/exit\n", true)
	if err != nil || !strings.Contains(errOut, "isn't installed") || strings.Contains(out, "Answering with big") {
		t.Fatalf("%v\n%s\n%s", err, out, errOut)
	}
}

func newFakeWith(offered []contracts.Model) *fakeChat {
	f := newFakeChat()
	f.offered = offered
	return f
}

// The answer's notice says when another model answered.
func TestChatShowsTheNotice(t *testing.T) {
	f := newFakeChat()
	f.msgs["0000000a"] = []contracts.Message{{Role: "assistant", Meta: &contracts.MessageMeta{Notice: "Answered with Small because Big ran out of memory."}}}
	out, errOut, err := runChat(t, f, []string{"hello"}, "/exit\n", true)
	if err != nil || !strings.Contains(errOut, "Answered with Small") || strings.Contains(out, "Answered with Small") {
		t.Fatalf("%v\nout: %s\nerr: %s", err, out, errOut)
	}
}

// Nothing installed does well at tasks: chat says which would (#1017).
func TestChatSuggestsATaskModel(t *testing.T) {
	task := contracts.Model{ID: "big", DisplayName: "Big", SizeBytes: 20_000_000_000, Tags: []string{"tasks"}, Fit: &contracts.ModelFit{Label: contracts.FitGood}}
	run := func(args []string, offered ...contracts.Model) string {
		t.Helper()
		f := newFakeChat()
		f.offered = offered
		var out, errOut bytes.Buffer
		streams := chatIO{in: strings.NewReader("/exit\n"), inTerminal: true, out: &out, outTerminal: true, errOut: &errOut}
		if err := chatCommand(args, streams, func() (chatAPI, error) { return eventsFake{f}, nil }); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	if got := run(nil, contracts.Model{ID: "small", Installed: true}, task); !strings.Contains(got, "toskarctl models install big") || !strings.Contains(got, "20.0 GB, good here") {
		t.Errorf("no suggestion: %q", got)
	}
	installed := task
	installed.Installed = true
	if got := run(nil, installed); strings.Contains(got, "goes much better") {
		t.Errorf("already installed: %q", got)
	}
	tooBig := task
	tooBig.Fit = &contracts.ModelFit{Label: contracts.FitTooLarge}
	small := contracts.Model{ID: "small", Installed: true}
	if got := run(nil, small, tooBig); !strings.Contains(got, "more than this computer has") || strings.Contains(got, "models install") {
		t.Errorf("too large: %q", got)
	}
	tooBig.Fit = &contracts.ModelFit{Label: contracts.FitUnsupported}
	if got := run(nil, small, tooBig); !strings.Contains(got, "more than this computer has") {
		t.Errorf("unsupported: %q", got)
	}
	if got := run([]string{"--model", "small"}, contracts.Model{ID: "small", Installed: true}, task); strings.Contains(got, "goes much better") {
		t.Errorf("a person who chose a model: %q", got)
	}
}

// A long command shows its output as it comes, without its control codes, and
// Ctrl-C stops the work and leaves the chat open (#1017).
func TestChatLongCommand(t *testing.T) {
	f := newFakeChat()
	var out, errOut bytes.Buffer
	in, typing := io.Pipe()
	interrupts := make(chan os.Signal, 1)
	streams := chatIO{in: in, inTerminal: true, out: &out, outTerminal: true, errOut: &errOut, interrupts: interrupts}
	done := make(chan error, 1)
	go func() { done <- chatCommand(nil, streams, func() (chatAPI, error) { return eventsFake{f}, nil }) }()

	fmt.Fprintln(typing, "long build")
	// The output arrives before anyone stops it.
	time.Sleep(200 * time.Millisecond)
	interrupts <- os.Interrupt
	time.Sleep(200 * time.Millisecond)
	fmt.Fprintln(typing, "and next")
	time.Sleep(200 * time.Millisecond)
	fmt.Fprintln(typing, "/exit")
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the chat didn't end")
	}
	got := out.String()
	for _, want := range []string{"  compiling a", "  linking b", "  30%", "Stopped."} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "[32m") || strings.Contains(got, "10%") {
		t.Errorf("control codes or an old progress state shown: %q", got)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.stopped) != 1 {
		t.Errorf("stopped %v", f.stopped)
	}
	if len(f.sent) != 2 || f.sent[1].Message != "and next" {
		t.Errorf("the chat should go on after Ctrl-C: %d messages", len(f.sent))
	}
}

// Ctrl-C at a question says no and stops the turn, rather than waiting for a
// line nobody will type.
func TestChatInterruptAtQuestion(t *testing.T) {
	f := newFakeChat()
	var out, errOut bytes.Buffer
	in, typing := io.Pipe()
	interrupts := make(chan os.Signal, 1)
	streams := chatIO{in: in, inTerminal: true, out: &out, outTerminal: true, errOut: &errOut, interrupts: interrupts}
	done := make(chan error, 1)
	go func() { done <- chatCommand(nil, streams, func() (chatAPI, error) { return eventsFake{f}, nil }) }()
	fmt.Fprintln(typing, "wipe it")
	time.Sleep(200 * time.Millisecond)
	interrupts <- os.Interrupt
	time.Sleep(200 * time.Millisecond)
	fmt.Fprintln(typing, "/exit")
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the chat didn't end")
	}
	if len(f.decisions) != 1 || !strings.Contains(f.decisions[0], "deny") {
		t.Errorf("decisions %v", f.decisions)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.stopped) != 1 {
		t.Errorf("stopped %v", f.stopped)
	}
}

// Ctrl-C at the prompt leaves.
func TestChatInterruptAtPrompt(t *testing.T) {
	f := newFakeChat()
	var out, errOut bytes.Buffer
	in, _ := io.Pipe()
	interrupts := make(chan os.Signal, 1)
	streams := chatIO{in: in, inTerminal: true, out: &out, outTerminal: true, errOut: &errOut, interrupts: interrupts}
	done := make(chan error, 1)
	go func() { done <- chatCommand(nil, streams, func() (chatAPI, error) { return eventsFake{f}, nil }) }()
	time.Sleep(200 * time.Millisecond)
	interrupts <- os.Interrupt
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Ctrl-C at the prompt should leave")
	}
}

func TestPlainOutput(t *testing.T) {
	for in, want := range map[string]string{
		"plain":                    "plain",
		"\x1b[1;31mred\x1b[0m":     "red",
		"\x1b]0;title\x07text":     "text",
		"10%\r50%\rdone\r":         "done",
		"tab\there":                "tab\there",
		"bell\x07 and \x08back":    "bell and back",
		"\x1b[2J\x1b[Hscreen wipe": "screen wipe",
	} {
		if got := plainOutput(in); got != want {
			t.Errorf("plainOutput(%q) = %q, want %q", in, got, want)
		}
	}
}

func fit(label contracts.FitLabel) *contracts.ModelFit { return &contracts.ModelFit{Label: label} }

func TestFirstModel(t *testing.T) {
	small := contracts.Model{ID: "small", SizeBytes: 1, Fit: fit(contracts.FitGood)}
	tight := contracts.Model{ID: "tight", SizeBytes: 1, Fit: fit("tight")}
	tasks := contracts.Model{ID: "tasks", SizeBytes: 9, Fit: fit(contracts.FitGood), Tags: []string{taskModelTag}}
	tooBig := contracts.Model{ID: "big", SizeBytes: 9, Tags: []string{taskModelTag}, Fit: fit(contracts.FitTooLarge)}
	unsure := contracts.Model{ID: "unsure", SizeBytes: 1}
	coder := contracts.Model{ID: "coder", SizeBytes: 5, Fit: fit(contracts.FitGood)}
	for name, c := range map[string]struct {
		list []contracts.Model
		want string
	}{
		"tasks first":     {[]contracts.Model{small, tasks}, "tasks"},
		"tasks must fit":  {[]contracts.Model{small, tooBig}, "small"},
		"fits well first": {[]contracts.Model{tight, small}, "small"},
		"any that runs":   {[]contracts.Model{tight, tooBig}, "tight"},
		"recommended":     {[]contracts.Model{small, coder}, "coder"},
		"tasks over it":   {[]contracts.Model{coder, tasks}, "tasks"},
		"none":            {[]contracts.Model{tooBig, unsure}, ""},
	} {
		got := ""
		if m := firstModel(c.list, "coder"); m != nil {
			got = m.ID
		}
		if got != c.want {
			t.Errorf("%s: %q", name, got)
		}
	}
}

// Starting from nothing: it says what it would install, and only with a yes.
func TestChatNoModelYet(t *testing.T) {
	offer := []contracts.Model{{ID: "small", DisplayName: "Small", SizeBytes: 1 << 30, Fit: fit(contracts.FitGood)}}

	f := newFakeWith(offer)
	out, _, err := runChat(t, f, nil, "y\n/exit\n", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Toskar has no model yet") || !strings.Contains(out, "Small (") || len(f.installed) != 1 || f.installed[0] != "small" {
		t.Errorf("out %q, installed %v", out, f.installed)
	}

	f = newFakeWith(offer)
	if _, _, err = runChat(t, f, nil, "n\n", true); err == nil || !strings.Contains(err.Error(), "Nothing was installed") {
		t.Errorf("no: %v", err)
	}
	if len(f.installed) != 0 {
		t.Errorf("installed without a yes: %v", f.installed)
	}

	// Nobody to ask: the command to run, and nothing installed.
	f = newFakeWith(offer)
	if _, _, err = runChat(t, f, []string{"hello"}, "", false); err == nil || !strings.Contains(err.Error(), "toskarctl models install small") {
		t.Errorf("piped: %v", err)
	}
	if len(f.installed) != 0 {
		t.Errorf("installed with nobody to ask: %v", f.installed)
	}

	f = newFakeWith([]contracts.Model{{ID: "big", Fit: fit(contracts.FitTooLarge)}})
	if _, _, err = runChat(t, f, nil, "", true); err == nil || !strings.Contains(err.Error(), "can't run any") {
		t.Errorf("too large: %v", err)
	}

	// A model named, or one there already, isn't asked about.
	f = newFakeWith([]contracts.Model{{ID: "here", Installed: true}, {ID: "small", Fit: fit(contracts.FitGood)}})
	if out, _, err = runChat(t, f, nil, "/exit\n", true); err != nil || strings.Contains(out, "no model yet") {
		t.Errorf("installed: %q %v", out, err)
	}
}

func TestStepLines(t *testing.T) {
	for _, c := range [][3]string{
		{"terminal", "go test ./...", "Running go test ./..."},
		{"filesystem.read", "sum.go", "Reading sum.go"},
		{"filesystem.write", "sum.go", "Writing sum.go"},
		{"internet.search", "weather", "Searching the web for weather"},
		{"git.status", "", "Checking what changed"},
		{"files.create", "", "Making a file"},
		{"mcp.thing", "did it", "did it"},
		{"mcp.thing", "", "mcp.thing"},
	} {
		if got := stepLine(c[0], c[1]); got != c[2] {
			t.Errorf("%v: %q", c, got)
		}
	}
	for _, c := range []struct {
		p    map[string]any
		want string
	}{
		{map[string]any{"duration_ms": 100.0}, ""},
		{map[string]any{"duration_ms": 12000.0}, "That took 12s"},
		{map[string]any{"exit": 1.0, "duration_ms": 100.0}, "That failed with exit code 1"},
		{map[string]any{"exit": 2.0, "duration_ms": 7000.0}, "That failed with exit code 2, after 7s"},
	} {
		if got := stepEnded(c.p); got != c.want {
			t.Errorf("%v: %q", c.p, got)
		}
	}
}
