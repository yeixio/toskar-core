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
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	toskar "github.com/yeixio/toskar-sdks/go"
	"github.com/yeixio/toskar-sdks/go/api"
	"golang.org/x/text/width"

	"github.com/yeixio/toskar-core/internal/askdata"
	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

const chatUsage = `usage: toskarctl chat [flags] [message]
  Chats with Toskar in the terminal (#959): the same chats as the apps, so
  one begun here continues on the phone and back. Answers stream as they're
  written. A tool that asks first, such as running code, shows what it
  would do and waits for y (yes), n (no), or a (always, in this chat).

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
  /log            what ran in this chat, its result, and who allowed it
  /help           this help
  /exit           leave (or Ctrl-D)`

// chatAPI is what chat asks of Toskar; a test stands in for it.
type chatAPI interface {
	createConversation(profile, model string) (contracts.Conversation, error)
	conversations() ([]contracts.Conversation, error)
	messages(id string) ([]contracts.Message, error)
	// models are the models Toskar offers, with what's installed.
	models() ([]contracts.Model, error)
	// runs are the tool calls in a chat, newest first.
	runs(id string) ([]toolRun, error)
	stream(ctx context.Context, req api.ChatJSONRequestBody) iter.Seq2[toskar.ChatStreamEvent, error]
	events(ctx context.Context) iter.Seq2[api.Event, error]
	decide(requestID string, allow, forChat bool) error
	// stop ends a chat's running turn and the commands in it.
	stop(id string) error
	// computer is the Toskar computer's name when it isn't this one, where
	// a tool such as the terminal runs.
	computer() string
}

// toolRun is one tool call on record.
type toolRun struct {
	At       time.Time `json:"at"`
	ToolID   string    `json:"tool_id"`
	Status   string    `json:"status"`
	Approval string    `json:"approval"`
	Summary  string    `json:"summary"`
	Error    string    `json:"error"`
}

// chatIO is where chat reads and writes. Piped input is data for one
// answer; output that isn't a terminal gets the answers' text only.
type chatIO struct {
	in          io.Reader
	inTerminal  bool
	out         io.Writer
	outTerminal bool
	errOut      io.Writer
	// size is the terminal's columns and rows, zero when it can't say.
	size func() (cols, rows int)
	// interrupts are Ctrl-C.
	interrupts <-chan os.Signal
}

// daemonChat is chatAPI on the daemon: its REST routes, and the SDK for
// the chat and event streams.
type daemonChat struct {
	rest daemonClient
	sdk  *toskar.Client
}

func newDaemonChat() (chatAPI, error) {
	d := newDaemon()
	options := []toskar.Option{toskar.WithURL(d.base), toskar.WithKey(config.Env("API_KEY")), toskar.WithTimeout(0)}
	chat := daemonChat{rest: d}
	// On this computer, Toskar works in the folder you're in. On another,
	// this folder means nothing there.
	if dir, err := os.Getwd(); err == nil && chat.computer() == "" {
		options = append(options, toskar.WithHeader(contracts.ProjectHeader, url.PathEscape(dir)))
	}
	sdk, err := toskar.New(options...)
	if err != nil {
		return nil, err
	}
	chat.sdk = sdk
	return chat, nil
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

func (d daemonChat) stop(id string) error {
	return d.rest.request(http.MethodPost, "/chat/stop", map[string]string{"conversation_id": id}, nil)
}

func (d daemonChat) runs(id string) ([]toolRun, error) {
	var list []toolRun
	err := d.rest.request(http.MethodGet, "/tools/runs?limit=500&conversation_id="+url.QueryEscape(id), nil, &list)
	return list, err
}

func (d daemonChat) models() ([]contracts.Model, error) {
	var list []contracts.Model
	err := d.rest.request(http.MethodGet, "/models", nil, &list)
	return list, err
}

func (d daemonChat) stream(ctx context.Context, req api.ChatJSONRequestBody) iter.Seq2[toskar.ChatStreamEvent, error] {
	return d.sdk.ChatStream(ctx, req)
}

func (d daemonChat) events(ctx context.Context) iter.Seq2[api.Event, error] {
	return d.sdk.Events(ctx)
}

func (d daemonChat) decide(requestID string, allow, forChat bool) error {
	return d.rest.request(http.MethodPost, "/tools/decide", map[string]any{"request_id": requestID, "allow": allow, "allow_session": forChat}, nil)
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
	// commandOutput is what a running command writes, and outputLine what's
	// come of a line it hasn't finished.
	commandOutput chan string
	outputLine    string
	// typed are the lines typed, closed at the end of input.
	typed chan string
	// stopping says Ctrl-C has asked the running turn to stop.
	stopping bool
}

// toolAsk is a tool waiting for a yes or no (tool.requested).
type toolAsk struct {
	requestID, tool, reason string
	args                    map[string]any
	// alwaysAsks is a command no earlier yes covers, and cantUndo says it
	// can't be undone (#1017).
	alwaysAsks, cantUndo bool
	// afterOutside says it asks because the chat just read something from
	// outside, such as a web page.
	afterOutside bool
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
	if err := checkModelInstalled(client, *model); err != nil {
		return err
	}
	s := &chatSession{api: client, io: streams, lines: bufio.NewScanner(streams.in), model: *model, profile: *profile, asks: make(chan toolAsk, 4), status: make(chan string, 8)}
	s.lines.Buffer(make([]byte, 0, 64<<10), 1<<20)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.commandOutput = make(chan string, 64)
	s.typed = make(chan string)
	go s.readInput(ctx)
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
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && r != '-' {
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
	s.suggestTaskModel()
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
			line, ok, _ := s.readLine()
			if !ok {
				return io.EOF
			}
			n, err := strconv.Atoi(strings.TrimSpace(line))
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
		// A Ctrl-C that came as a turn ended isn't for the prompt.
		select {
		case <-s.io.interrupts:
		default:
		}
		fmt.Fprint(s.io.out, s.prompt())
		typed, ok, _ := s.readLine()
		if !ok {
			fmt.Fprintln(s.io.out)
			return s.lines.Err()
		}
		line := strings.TrimSpace(typed)
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
		if err := checkModelInstalled(s.api, arg); err != nil {
			return false, err
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
	case "log":
		return false, s.log()
	default:
		return false, fmt.Errorf("there's no /%s; /help lists the commands", name)
	}
	return false, nil
}

// showNotice says what the answer's notice says, such as that another model
// answered because the one asked for couldn't. It goes to standard error, so a
// pipe still gets only the answer.
func (s *chatSession) showNotice() {
	msgs, err := s.api.messages(s.conv)
	if err != nil || len(msgs) == 0 {
		return
	}
	if last := msgs[len(msgs)-1]; last.Role == "assistant" && last.Meta != nil && last.Meta.Notice != "" {
		fmt.Fprintln(s.io.errOut, s.dim(last.Meta.Notice))
	}
}

// suggestTaskModel says when nothing installed does well at work such as fixing
// code, and which model would: the difference is large (#1017). A person who
// named a model has chosen.
func (s *chatSession) suggestTaskModel() {
	if s.model != "" || !s.io.outTerminal {
		return
	}
	list, err := s.api.models()
	if err != nil {
		return
	}
	var best *contracts.Model
	for i, m := range list {
		if !slices.Contains(m.Tags, taskModelTag) {
			continue
		}
		if m.Installed {
			return
		}
		if best == nil {
			best = &list[i]
		}
	}
	if best == nil {
		return
	}
	switch fit := best.Fit; {
	case fit == nil || fit.Label == "" || fit.Label == contracts.FitTooLarge || fit.Label == contracts.FitUnsupported:
		s.note(fmt.Sprintf("Work such as fixing code goes much better with %s, which needs more than this computer has. Smaller models often can't finish it. A connected computer may run it.", best.DisplayName))
	default:
		s.note(fmt.Sprintf("Work such as fixing code goes much better with %s (%s, %s here). Install it with: toskarctl models install %s", best.DisplayName, sizeText(best.SizeBytes), fit.Label, best.ID))
	}
}

// taskModelTag marks a catalog model measured to finish tasks (#1034).
const taskModelTag = "tasks"

// checkModelInstalled refuses a model Toskar has but hasn't installed: asked
// for one, you'd otherwise get an answer from another and not know it. Models
// it doesn't list, such as an external server's, are for it to find or refuse.
func checkModelInstalled(api chatAPI, id string) error {
	if id == "" || id == "auto" {
		return nil
	}
	list, err := api.models()
	if err != nil {
		return nil
	}
	for _, m := range list {
		if m.ID == id && !m.Installed {
			return &exitError{code: 2, msg: fmt.Sprintf("%s isn't installed. Install it with: toskarctl models install %s", id, id)}
		}
	}
	return nil
}

// log prints what ran in this chat, oldest first (#1017).
func (s *chatSession) log() error {
	runs, err := s.api.runs(s.conv)
	if err != nil {
		return err
	}
	if len(runs) == 0 {
		s.note("Nothing has run in this chat yet.")
		return nil
	}
	for _, r := range slices.Backward(runs) {
		line := fmt.Sprintf("%s  %s", r.At.Local().Format("15:04:05"), r.ToolID)
		if r.Summary != "" {
			line += "  " + r.Summary
		}
		fmt.Fprintln(s.io.out, line)
		fmt.Fprintln(s.io.out, s.dim("  "+runOutcome(r)))
	}
	return nil
}

// runOutcome says how a call ended and who allowed it.
func runOutcome(r toolRun) string {
	result := r.Status
	switch {
	case r.Status == "denied" && r.Approval == "you":
		return "not run: you said no"
	case r.Status == "denied":
		return "not run: " + r.Error
	case r.Error != "":
		result = "failed: " + r.Error
	}
	who := map[string]string{"profile": "allowed by the profile", "you": "you said yes", "session": "you said always"}[r.Approval]
	if who == "" {
		return result
	}
	return result + ", " + who
}

// turn sends one message and streams its answer. A tool that asks first
// waits for a yes or no here, read in this goroutine with the rest of the
// input, so the question and the next message never share a line.
func (s *chatSession) turn(ctx context.Context, text string) error {
	s.stopping = false
	if len(s.pending) > 0 {
		text = askdata.Message(text, strings.Join(s.pending, "\n\n"))
		s.pending = nil
	}
	req := api.ChatJSONRequestBody{Message: text, ConversationId: &s.conv, Client: ptrTo("terminal"), TimeZone: ptrTo(time.Local.String())}
	// Someone reading sees the answer as it's written (#1006). A pipe gets it
	// whole, since text can't be taken back from a file.
	if s.io.outTerminal {
		req.Live = ptrTo(true)
	}
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
	// shown is the answer so far that can still be erased from the screen.
	shown := ""
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
			s.endOutputLine()
			switch it.e.Type {
			case "token":
				fmt.Fprint(s.io.out, it.e.Content)
				shown += it.e.Content
				wrote = true
			case "replace":
				if !s.erase(shown) && wrote {
					fmt.Fprintln(s.io.out)
				}
				fmt.Fprint(s.io.out, it.e.Content)
				shown = it.e.Content
				wrote = it.e.Content != ""
			case "done":
				if wrote {
					fmt.Fprintln(s.io.out)
				}
				s.showNotice()
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
			s.endOutputLine()
			// What's above the question stays, as the reason for it.
			shown = ""
			s.ask(a)
		case st := <-s.status:
			s.endOutputLine()
			if s.io.outTerminal && !wrote {
				fmt.Fprintln(s.io.out, s.dim("· "+st))
			}
		case text := <-s.commandOutput:
			if !s.io.outTerminal {
				continue
			}
			if wrote {
				fmt.Fprintln(s.io.out)
				wrote = false
			}
			shown = ""
			s.showOutput(text)
		case <-s.io.interrupts:
			// A second Ctrl-C doesn't wait for the turn to end.
			if s.stopping {
				s.endOutputLine()
				return nil
			}
			if wrote {
				fmt.Fprintln(s.io.out)
				wrote = false
			}
			shown = ""
			s.stopTurn()
		}
	}
}

// stopTurn asks Toskar to stop this chat's running turn and its commands. The
// turn ends with what was written, and the chat stays open.
func (s *chatSession) stopTurn() {
	s.stopping = true
	s.endOutputLine()
	s.note("Stopping. Press Ctrl-C again not to wait.")
	go func() {
		if err := s.api.stop(s.conv); err != nil {
			fmt.Fprintln(s.io.errOut, err)
		}
	}()
}

// controlCodes are a terminal's codes for color and moving around, which a
// command's output has no business sending to this screen.
var controlCodes = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\\\)|\x1b.`)

// showOutput shows what a command wrote, faintly and a line at a time.
func (s *chatSession) showOutput(text string) {
	s.outputLine += text
	for {
		line, rest, found := strings.Cut(s.outputLine, "\n")
		if !found {
			return
		}
		s.outputLine = rest
		s.printOutputLine(line)
	}
}

// endOutputLine shows a line the command hasn't finished, before anything else
// goes on the screen.
func (s *chatSession) endOutputLine() {
	if s.outputLine != "" {
		s.printOutputLine(s.outputLine)
		s.outputLine = ""
	}
}

func (s *chatSession) printOutputLine(line string) {
	fmt.Fprintln(s.io.out, s.dim("  "+plainOutput(line)))
}

// plainOutput is a line of a command's output with its control codes taken
// out. A progress bar rewrites its line with returns, and only its last state
// is kept.
func plainOutput(line string) string {
	line = controlCodes.ReplaceAllString(line, "")
	line = strings.TrimRight(line, "\r")
	if i := strings.LastIndexByte(line, '\r'); i >= 0 {
		line = line[i+1:]
	}
	return strings.Map(func(r rune) rune {
		if r != '\t' && unicode.IsControl(r) {
			return -1
		}
		return r
	}, line)
}

// readInput passes on what's typed, so that waiting for it can be interrupted.
func (s *chatSession) readInput(ctx context.Context) {
	defer close(s.typed)
	for s.lines.Scan() {
		select {
		case s.typed <- s.lines.Text():
		case <-ctx.Done():
			return
		}
	}
}

// readLine waits for a line. ok is false at the end of input or on Ctrl-C,
// and interrupted says which.
func (s *chatSession) readLine() (line string, ok, interrupted bool) {
	select {
	case line, ok = <-s.typed:
		return line, ok, false
	case <-s.io.interrupts:
		return "", false, true
	}
}

// erase clears text just written from the screen, and says whether it could:
// not when the terminal can't say its size, or some of it has scrolled away.
func (s *chatSession) erase(text string) bool {
	if text == "" || s.io.size == nil {
		return false
	}
	cols, rows := s.io.size()
	if cols <= 0 || rows <= 0 {
		return false
	}
	n := rowsOf(text, cols)
	if n >= rows {
		return false
	}
	fmt.Fprint(s.io.out, "\r")
	if n > 1 {
		fmt.Fprintf(s.io.out, "\x1b[%dA", n-1)
	}
	fmt.Fprint(s.io.out, "\x1b[J")
	return true
}

// rowsOf is how many rows text takes on a terminal this many columns wide.
func rowsOf(text string, cols int) int {
	n := 0
	for _, line := range strings.Split(text, "\n") {
		n += max(1, (cellsOf(line)+cols-1)/cols)
	}
	return n
}

// cellsOf is how many columns a line takes: two for wide characters such as
// Japanese, and none for marks that combine with the one before.
func cellsOf(line string) int {
	n := 0
	for _, r := range line {
		switch {
		case r == '\t':
			n += 8 - n%8
		case unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r):
		case isWide(r):
			n += 2
		default:
			n++
		}
	}
	return n
}

func isWide(r rune) bool {
	k := width.LookupRune(r).Kind()
	return k == width.EastAsianWide || k == width.EastAsianFullwidth
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
	if a.cantUndo {
		fmt.Fprintln(s.io.out, "This can't be undone.")
	}
	if a.afterOutside {
		fmt.Fprintln(s.io.out, "This chat just read something from outside, such as a web page. Check that this is what you asked for.")
	}
	allow, forChat := false, false
	if !s.io.inTerminal {
		fmt.Fprintln(s.io.out, "Not allowed: there's no one at the terminal to ask. Chat without a pipe to answer.")
	} else {
		if a.alwaysAsks {
			fmt.Fprint(s.io.out, "Allow? [y]es, [n]o (this one asks every time): ")
		} else {
			if a.tool == "terminal" {
				s.note("Always also lets known commands such as make and npm run keep going, and those run the project's own code. Choose it only for a project you trust.")
			}
			fmt.Fprint(s.io.out, "Allow? [y]es, [n]o, [a]lways in this chat: ")
		}
		line, ok, interrupted := s.readLine()
		if ok {
			switch strings.ToLower(strings.TrimSpace(line)) {
			case "y", "yes":
				allow = true
			case "a", "always":
				// An "a" where it isn't offered isn't a yes.
				allow, forChat = !a.alwaysAsks, !a.alwaysAsks
			}
		}
		if interrupted {
			fmt.Fprintln(s.io.out)
			s.stopTurn()
		}
	}
	if err := s.api.decide(a.requestID, allow, forChat); err != nil {
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
			a.alwaysAsks, _ = p["always_asks"].(bool)
			a.cantUndo, _ = p["cant_undo"].(bool)
			a.afterOutside, _ = p["after_outside"].(bool)
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
		case "tool.output":
			if text, _ := p["text"].(string); text != "" {
				select {
				case s.commandOutput <- text:
				default:
				}
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
