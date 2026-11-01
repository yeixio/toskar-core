package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"iter"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	toskar "github.com/yeixio/toskar-sdks/go"
	"github.com/yeixio/toskar-sdks/go/api"

	"github.com/yeixio/toskar-core/internal/askdata"
	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

const chatUsage = `usage: toskarctl chat [flags] [message]
  Chats with Toskar in the terminal (#959): the same chats as the apps, so
  one begun here continues on the phone and back. Answers stream as they're
  written. A tool that asks first, such as running code, shows what it
  would do and waits for y (yes) or n (no).

  Piped in, it answers once, with what's piped as data to work on:
    make 2>&1 | toskarctl chat "Why did this fail?"

flags:
  --resume [id]   continue a chat: its id, or pick from recent ones
  --model id      a model, or auto (the default)
  --profile id    answer as this profile
  --file path     data to work on with the first message

while chatting:
  /new            start a new chat
  /model id       answer with another model (auto to let Toskar pick)
  /profile id     answer as another profile
  /attach path    send a file as data with the next message
  /help           this help
  /exit           leave (or Ctrl-D)`

// chatAPI is what chat asks of Toskar; a test stands in for it.
type chatAPI interface {
	createConversation(profile, model string) (contracts.Conversation, error)
	conversations() ([]contracts.Conversation, error)
	messages(id string) ([]contracts.Message, error)
	stream(ctx context.Context, req api.ChatJSONRequestBody) iter.Seq2[toskar.ChatStreamEvent, error]
	events(ctx context.Context) iter.Seq2[api.Event, error]
	decide(requestID string, allow bool) error
	// computer is the Toskar computer's name when it isn't this one, where
	// a tool such as the terminal runs.
	computer() string
}

// chatIO is where chat reads and writes. Piped input is data for one
// answer; output that isn't a terminal gets the answers' text only.
type chatIO struct {
	in          io.Reader
	inTerminal  bool
	out         io.Writer
	outTerminal bool
	errOut      io.Writer
}

// daemonChat is chatAPI on the daemon: its REST routes, and the SDK for
// the chat and event streams.
type daemonChat struct {
	rest daemonClient
	sdk  *toskar.Client
}

func newDaemonChat() (chatAPI, error) {
	d := newDaemon()
	sdk, err := toskar.New(toskar.WithURL(d.base), toskar.WithKey(config.Env("API_KEY")), toskar.WithTimeout(0))
	if err != nil {
		return nil, err
	}
	return daemonChat{rest: d, sdk: sdk}, nil
}

func (d daemonChat) computer() string {
	u, err := url.Parse(d.rest.base)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	if host == "localhost" {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return ""
	}
	return host
}

func (d daemonChat) createConversation(profile, model string) (contracts.Conversation, error) {
	var c contracts.Conversation
	body := map[string]string{"title": "Terminal chat"}
	if profile != "" {
		body["profile_id"] = profile
	}
	if model != "" && model != "auto" {
		body["model_id"] = model
	}
	err := d.rest.request(http.MethodPost, "/conversations", body, &c)
	return c, err
}

func (d daemonChat) conversations() ([]contracts.Conversation, error) {
	var list []contracts.Conversation
	err := d.rest.request(http.MethodGet, "/conversations", nil, &list)
	return list, err
}

func (d daemonChat) messages(id string) ([]contracts.Message, error) {
	var list []contracts.Message
	err := d.rest.request(http.MethodGet, "/conversations/"+id+"/messages", nil, &list)
	return list, err
}

func (d daemonChat) stream(ctx context.Context, req api.ChatJSONRequestBody) iter.Seq2[toskar.ChatStreamEvent, error] {
	return d.sdk.ChatStream(ctx, req)
}

func (d daemonChat) events(ctx context.Context) iter.Seq2[api.Event, error] {
	return d.sdk.Events(ctx)
}

func (d daemonChat) decide(requestID string, allow bool) error {
	return d.rest.request(http.MethodPost, "/tools/decide", map[string]any{"request_id": requestID, "allow": allow}, nil)
}

// chatSession is one terminal chat.
type chatSession struct {
	api     chatAPI
	io      chatIO
	lines   *bufio.Scanner
	conv    string
	model   string
	profile string
	// pending is data from /attach for the next message.
	pending []string
	// asks are the tool approvals this chat's turns wait on.
	asks chan toolAsk
	// status is a tool's progress for the running turn.
	status chan string
}

// toolAsk is a tool waiting for a yes or no (tool.requested).
type toolAsk struct {
	requestID, tool, reason string
	args                    map[string]any
}

func chatCommand(args []string, streams chatIO, newAPI func() (chatAPI, error)) error {
	fs := flag.NewFlagSet("chat", flag.ContinueOnError)
	fs.SetOutput(new(bytes.Buffer))
	model := fs.String("model", "", "")
	profile := fs.String("profile", "", "")
	file := fs.String("file", "", "")
	resume := fs.Bool("resume", false, "")
	flagArgs, resumeID := resumeArg(args)
	if err := fs.Parse(flagArgs); err != nil {
		return &exitError{code: 2, msg: chatUsage}
	}
	rest := fs.Args()
	first := strings.TrimSpace(strings.Join(rest, " "))
	data := ""
	if *file != "" {
		raw, err := os.ReadFile(*file)
		if err != nil {
			return &exitError{code: 2, msg: err.Error()}
		}
		data = string(raw)
	}
	if !streams.inTerminal {
		raw, err := io.ReadAll(streams.in)
		if err != nil {
			return err
		}
		data = strings.TrimSpace(data + "\n" + string(raw))
		if first == "" {
			return &exitError{code: 2, msg: "Piped in, chat needs a message to go with it, such as: make 2>&1 | toskarctl chat \"Why did this fail?\""}
		}
	}

	client, err := newAPI()
	if err != nil {
		return err
	}
	s := &chatSession{api: client, io: streams, lines: bufio.NewScanner(streams.in), model: *model, profile: *profile, asks: make(chan toolAsk, 4), status: make(chan string, 8)}
	s.lines.Buffer(make([]byte, 0, 64<<10), 1<<20)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.watch(ctx)

	switch {
	case *resume:
		if err := s.resume(resumeID); err != nil {
			return err
		}
	default:
		if err := s.newChat(); err != nil {
			return err
		}
	}
	if data != "" {
		s.pending = append(s.pending, data)
	}
	if first != "" {
		if err := s.turn(ctx, first); err != nil {
			return err
		}
	}
	if !streams.inTerminal {
		fmt.Fprintf(streams.errOut, "Continue this chat: toskarctl chat --resume %s\n", s.conv)
		return nil
	}
	return s.loop(ctx)
}

// resumeArg takes the id after --resume out of args, since flag's
// booleans take no value: the args for flag, and the id or "".
func resumeArg(args []string) ([]string, string) {
	out := make([]string, 0, len(args))
	id := ""
	for i := 0; i < len(args); i++ {
		if (args[i] == "--resume" || args[i] == "-resume") && i+1 < len(args) && looksLikeID(args[i+1]) {
			out = append(out, "--resume")
			id = args[i+1]
			i++
			continue
		}
		out = append(out, args[i])
	}
	return out, id
}

// looksLikeID reports a conversation id: hex with dashes, not a message.
func looksLikeID(s string) bool {
	if len(s) < 8 || strings.ContainsAny(s, " \t") {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r == '-') {
			return false
		}
	}
	return true
}

func (s *chatSession) newChat() error {
	c, err := s.api.createConversation(s.profile, s.model)
	if err != nil {
		return err
	}
	s.conv = c.ID
	s.note("New chat. /help for commands.")
	return nil
}

// resume picks up a chat: id, or one picked from the recent ones.
func (s *chatSession) resume(id string) error {
	if id == "" {
		list, err := s.api.conversations()
		if err != nil {
			return err
		}
		if len(list) == 0 {
			return fmt.Errorf("there are no chats to continue yet: run toskarctl chat to start one")
		}
		sort.SliceStable(list, func(i, j int) bool { return list[i].UpdatedAt.After(list[j].UpdatedAt) })
		list = list[:min(len(list), 10)]
		if !s.io.inTerminal {
			id = list[0].ID
		} else {
			for i, c := range list {
				fmt.Fprintf(s.io.out, "%2d. %s  %s\n", i+1, titleOf(c), s.dim(c.UpdatedAt.Local().Format("Jan 2 15:04")))
			}
			fmt.Fprint(s.io.out, "Continue which? ")
			if !s.lines.Scan() {
				return io.EOF
			}
			n, err := strconv.Atoi(strings.TrimSpace(s.lines.Text()))
			if err != nil || n < 1 || n > len(list) {
				return fmt.Errorf("pick a number from 1 to %d", len(list))
			}
			id = list[n-1].ID
		}
	}
	s.conv = id
	msgs, err := s.api.messages(id)
	if err != nil {
		return err
	}
	// The last exchange, so the chat picks up where it was.
	for _, m := range msgs[max(0, len(msgs)-2):] {
		who := "You"
		if m.Role == "assistant" {
			who = "Toskar"
		}
		text := m.Content
		if m.Role == "user" {
			if prompt, data := askdata.Prompt(text); data {
				text = prompt + " " + s.dim("(with data)")
			}
		}
		fmt.Fprintf(s.io.out, "%s %s\n", s.dim(who+":"), text)
	}
	s.note("Continuing. /help for commands.")
	return nil
}

func titleOf(c contracts.Conversation) string {
	if strings.TrimSpace(c.Title) == "" {
		return "Untitled chat"
	}
	return c.Title
}

// loop reads messages and commands until /exit or the end of input.
func (s *chatSession) loop(ctx context.Context) error {
	for {
		fmt.Fprint(s.io.out, s.prompt())
		if !s.lines.Scan() {
			fmt.Fprintln(s.io.out)
			return s.lines.Err()
		}
		line := strings.TrimSpace(s.lines.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "/") {
			done, err := s.command(line)
			if err != nil {
				fmt.Fprintln(s.io.errOut, err)
			}
			if done {
				return nil
			}
			continue
		}
		if err := s.turn(ctx, line); err != nil {
			fmt.Fprintln(s.io.errOut, err)
		}
	}
}

// command runs a /command; done says the chat is over.
func (s *chatSession) command(line string) (done bool, err error) {
	name, arg, _ := strings.Cut(strings.TrimPrefix(line, "/"), " ")
	arg = strings.TrimSpace(arg)
	switch name {
	case "exit", "quit":
		return true, nil
	case "help":
		fmt.Fprintln(s.io.out, chatUsage)
	case "new":
		return false, s.newChat()
	case "model":
		if arg == "" {
			return false, errors.New("/model needs a model id, or auto")
		}
		s.model = arg
		s.note("Answering with " + arg + ".")
	case "profile":
		if arg == "" {
			return false, errors.New("/profile needs a profile id")
		}
		s.profile = arg
		s.note("Answering as " + arg + ".")
	case "attach":
		raw, err := os.ReadFile(arg)
		if err != nil {
			return false, err
		}
		s.pending = append(s.pending, string(raw))
		s.note(fmt.Sprintf("Attached %s; it goes with your next message.", arg))
	default:
		return false, fmt.Errorf("there's no /%s; /help lists the commands", name)
	}
	return false, nil
}

// turn sends one message and streams its answer. A tool that asks first
// waits for a yes or no here, read in this goroutine with the rest of the
// input, so the question and the next message never share a line.
func (s *chatSession) turn(ctx context.Context, text string) error {
	if len(s.pending) > 0 {
		text = askdata.Message(text, strings.Join(s.pending, "\n\n"))
		s.pending = nil
	}
	req := api.ChatJSONRequestBody{Message: text, ConversationId: &s.conv, Client: ptrTo("terminal"), TimeZone: ptrTo(time.Local.String())}
	if s.model != "" {
		req.ModelId = ptrTo(s.model)
	}
	if s.profile != "" {
		req.ProfileId = ptrTo(s.profile)
	}
	type item struct {
		e   toskar.ChatStreamEvent
		err error
	}
	items := make(chan item)
	turnCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		defer close(items)
		for e, err := range s.api.stream(turnCtx, req) {
			select {
			case items <- item{e, err}:
			case <-turnCtx.Done():
				return
			}
		}
	}()
	wrote := false
	for {
		select {
		case it, ok := <-items:
			if !ok {
				return nil
			}
			if it.err != nil {
				if wrote {
					fmt.Fprintln(s.io.out)
				}
				return it.err
			}
			switch it.e.Type {
			case "token":
				fmt.Fprint(s.io.out, it.e.Content)
				wrote = true
			case "done":
				if wrote {
					fmt.Fprintln(s.io.out)
				}
				if m := it.e.Metrics; m != nil && s.io.outTerminal && m.EvalTokPerSec > 0 {
					fmt.Fprintln(s.io.out, s.dim(fmt.Sprintf("%.0f tokens/s", m.EvalTokPerSec)))
				}
				return nil
			}
		case a := <-s.asks:
			if wrote {
				fmt.Fprintln(s.io.out)
				wrote = false
			}
			s.ask(a)
		case st := <-s.status:
			if s.io.outTerminal && !wrote {
				fmt.Fprintln(s.io.out, s.dim("· "+st))
			}
		}
	}
}

// ask shows a tool waiting for permission, and sends the answer.
func (s *chatSession) ask(a toolAsk) {
	fmt.Fprintf(s.io.out, "%s wants to run %s", "Toskar", a.tool)
	if where := s.api.computer(); where != "" {
		fmt.Fprintf(s.io.out, " on %s", where)
	}
	// A chat's own tool calls all give the same reason, which says nothing.
	if a.reason != "" && a.reason != "chat requested tool" {
		fmt.Fprintf(s.io.out, ", %s", a.reason)
	}
	fmt.Fprintln(s.io.out, ":")
	if raw, err := json.MarshalIndent(a.args, "  ", "  "); err == nil && len(a.args) > 0 {
		fmt.Fprintf(s.io.out, "  %s\n", raw)
	}
	allow := false
	if !s.io.inTerminal {
		fmt.Fprintln(s.io.out, "Not allowed: there's no one at the terminal to ask. Chat without a pipe to answer.")
	} else {
		fmt.Fprint(s.io.out, "Allow? [y]es, [n]o: ")
		if s.lines.Scan() {
			answer := strings.ToLower(strings.TrimSpace(s.lines.Text()))
			allow = answer == "y" || answer == "yes"
		}
	}
	if err := s.api.decide(a.requestID, allow); err != nil {
		fmt.Fprintln(s.io.errOut, err)
	}
}

// watch follows Toskar's events for this chat: tools waiting for a yes or
// no, and tools at work, for the turn to show.
func (s *chatSession) watch(ctx context.Context) {
	for e, err := range s.api.events(ctx) {
		if err != nil {
			return
		}
		var p map[string]any
		if e.Payload != nil {
			p = *e.Payload
		}
		if conv, _ := p["conversation_id"].(string); conv != "" && conv != s.conv {
			continue
		}
		switch e.Type {
		case "tool.requested":
			a := toolAsk{}
			a.requestID, _ = p["request_id"].(string)
			a.tool, _ = p["tool_id"].(string)
			a.reason, _ = p["reason"].(string)
			a.args, _ = p["args"].(map[string]any)
			if a.requestID != "" {
				s.asks <- a
			}
		case "tool.started":
			summary, _ := p["summary"].(string)
			if summary == "" {
				summary, _ = p["tool_id"].(string)
			}
			select {
			case s.status <- summary:
			default:
			}
		}
	}
}

func (s *chatSession) prompt() string {
	if !s.io.outTerminal {
		return ""
	}
	return "› "
}

func (s *chatSession) note(text string) {
	if s.io.outTerminal {
		fmt.Fprintln(s.io.out, s.dim(text))
	}
}

// dim is text a terminal shows faintly, and plain otherwise.
func (s *chatSession) dim(text string) string {
	if !s.io.outTerminal {
		return text
	}
	return "\x1b[2m" + text + "\x1b[0m"
}

func ptrTo[T any](v T) *T { return &v }
