package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	toskar "github.com/yeixio/toskar-sdks/go"
	"github.com/yeixio/toskar-sdks/go/api"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// swarmTurn is a task swarm's turn (#1135): two agents in lanes, the second
// asking to run the tests from its own chat, then a merge with a conflict,
// and the merged answer.
func (f *fakeChat) swarmTurn(ctx context.Context, conv string, yield func(toskar.ChatStreamEvent, error) bool) {
	f.mu.Lock()
	if f.swarms == nil {
		f.swarms = map[string]contracts.Swarm{}
	}
	f.swarms["sw1"] = contracts.Swarm{ID: "sw1", ConversationID: conv, Mode: "split", Status: "done", Folder: "/work",
		Branch: "toskar/swarm/sw1/merged", Checks: "passed", Applied: true,
		Agents: []contracts.SwarmAgent{
			{Agent: 1, ConversationID: "agent-1", Part: "Write the sum function in sum.go", Status: "done", Seconds: 12},
			{Agent: 2, ConversationID: "agent-2", Part: "Write tests for the sum function", Status: "done", Seconds: 30},
		}}
	f.mu.Unlock()
	send := func(kind string, payload map[string]any) {
		if _, ok := payload["conversation_id"]; !ok {
			payload["conversation_id"] = conv
		}
		payload["swarm_id"] = "sw1"
		f.events <- api.Event{Type: kind, Payload: &payload}
		// Paced, so each change shows before the next.
		time.Sleep(30 * time.Millisecond)
	}
	send("swarm.started", map[string]any{"mode": "split", "agents": []any{
		map[string]any{"agent": 1.0, "conversation_id": "agent-1", "part": "code", "model": "qwen3.5-9b", "node_id": "n1", "status": "running"},
		map[string]any{"agent": 2.0, "conversation_id": "agent-2", "part": "tests", "model": "qwen3.5-4b", "node_id": "n2", "status": "queued"},
	}})
	send("swarm.agent.progress", map[string]any{"agent": 1.0, "step": 1.0, "tool": "filesystem.write", "summary": "sum.go"})
	send("swarm.agent.started", map[string]any{"agent": 2.0, "agent_conversation_id": "agent-2", "node_id": "n2", "model": "qwen3.5-4b"})
	// The agent asks in its own chat.
	send("tool.requested", map[string]any{"conversation_id": "agent-2", "agent": "Agent 2: tests", "agent_number": 2.0,
		"request_id": "req-a2", "tool_id": "terminal", "args": map[string]any{"command": "go test ./..."}})
	select {
	case <-f.decided:
	case <-time.After(5 * time.Second):
		yield(toskar.ChatStreamEvent{}, errors.New("no decision"))
		return
	case <-ctx.Done():
		return
	}
	send("swarm.agent.progress", map[string]any{"agent": 2.0, "step": 1.0, "tool": "terminal", "summary": "go test ./..."})
	send("swarm.agent.finished", map[string]any{"agent": 1.0, "status": "done", "seconds": 12.0})
	send("swarm.agent.finished", map[string]any{"agent": 2.0, "status": "done", "seconds": 30.0})
	send("swarm.merging", map[string]any{"mode": "split", "agent": 1.0, "branch": "toskar/swarm/sw1/agent-1"})
	send("swarm.merging", map[string]any{"mode": "split", "agent": 2.0, "branch": "toskar/swarm/sw1/agent-2",
		"conflict": map[string]any{"agent": 2.0, "branch": "toskar/swarm/sw1/agent-2", "files": []any{"sum.go"}}})
	// The events arrive before the answer, as they do from Core.
	time.Sleep(200 * time.Millisecond)
	if !yield(toskar.ChatStreamEvent{Type: "token", Content: "Merged answer"}, nil) {
		return
	}
	yield(toskar.ChatStreamEvent{Type: "done"}, nil)
}

func newSwarmFake() *fakeChat {
	f := newFakeChat()
	f.computers = []contracts.Node{{ID: "n1", Name: "laptop"}, {ID: "n2", Name: "rig"}}
	f.msgs["agent-2"] = []contracts.Message{{Role: "user", Content: "Write tests"}, {Role: "assistant", Content: "I wrote sum_test.go."}}
	return f
}

// runSwarmChat runs a chat in a terminal, with output to one too or not.
func runSwarmChat(t *testing.T, f *fakeChat, args []string, input string, outTerminal bool) (string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	streams := chatIO{in: strings.NewReader(input), inTerminal: true, out: &out, outTerminal: outTerminal, errOut: &errOut,
		size: func() (int, int) { return 120, 40 }}
	done := make(chan error, 1)
	go func() { done <- chatCommand(args, streams, func() (chatAPI, error) { return eventsFake{f}, nil }) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("%v\n%s\n%s", err, out.String(), errOut.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the chat didn't end")
	}
	return out.String(), errOut.String()
}

var escapes = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// On a terminal, a swarm's agents each have a line, redrawn in place as they
// work; an agent's question is labeled and answered here; then the answer,
// and what the task did with its change (#1135).
func TestChatSwarmLanesOnATerminal(t *testing.T) {
	f := newSwarmFake()
	out, errOut := runSwarmChat(t, f, []string{"--swarm"}, "fix the sums\ny\n/exit\n", true)
	plain := escapes.ReplaceAllString(out, "")
	for _, want := range []string{
		"Agent 1 · code · laptop · qwen3.5-9b · running · Writing sum.go",
		"Agent 2 · tests · rig · qwen3.5-4b · queued",
		"Agent 2: tests wants to run terminal:",
		`"command": "go test ./..."`,
		"Allow? [y]es, [n]o, [a]lways for this agent:",
		"Agent 2 · tests · rig · qwen3.5-4b · running · Running go test ./...",
		"Agent 1 · code · laptop · qwen3.5-9b · done in 12s",
		"Agent 2's change wasn't merged: it conflicts with the others in sum.go. It's kept on toskar/swarm/sw1/agent-2",
		"Merging agent 1's change from toskar/swarm/sw1/agent-1",
		"The change is on toskar/swarm/sw1/merged, and the checks passed. It's applied to /work as uncommitted changes.",
		"Merged answer",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("no %q in:\n%s", want, plain)
		}
	}
	// Redrawn in place: up over the lines, and clear below.
	if !regexp.MustCompile(`\r\x1b\[\d+A\x1b\[J`).MatchString(out) {
		t.Errorf("the lanes weren't redrawn in place: %q", out)
	}
	// The lanes come before the answer.
	if strings.LastIndex(plain, "done in 30s") > strings.Index(plain, "Merged answer") {
		t.Errorf("the lanes should stand above the answer:\n%s", plain)
	}
	if len(f.decisions) != 1 || f.decisions[0] != "req-a2 allow" {
		t.Errorf("decisions %v", f.decisions)
	}
	if len(f.sent) != 1 || f.sent[0].Swarm != "split" {
		t.Errorf("sent %+v", f.sent)
	}
	if errOut != "" {
		t.Errorf("errors: %s", errOut)
	}
}

// Piped out, each change is a plain line on standard error, and standard
// output gets only the answer.
func TestChatSwarmLanesPiped(t *testing.T) {
	f := newSwarmFake()
	out, errOut := runSwarmChat(t, f, []string{"--swarm", "race"}, "fix the sums\na\n/exit\n", false)
	if strings.Contains(errOut, "\x1b") || strings.Contains(out, "\x1b") {
		t.Errorf("control codes in plain output: %q %q", out, errOut)
	}
	lines := strings.Split(strings.TrimSpace(errOut), "\n")
	want := []string{
		"Agent 1 · code · laptop · qwen3.5-9b · running",
		"Agent 2 · tests · rig · qwen3.5-4b · queued",
		"Agent 1 · code · laptop · qwen3.5-9b · running · Writing sum.go",
		"Agent 2 · tests · rig · qwen3.5-4b · running",
		"Agent 2 · tests · rig · qwen3.5-4b · running · Running go test ./...",
		"Agent 1 · code · laptop · qwen3.5-9b · done in 12s",
		"Agent 2 · tests · rig · qwen3.5-4b · done in 30s",
		"Merging agent 1's change from toskar/swarm/sw1/agent-1",
		"Agent 2's change wasn't merged: it conflicts with the others in sum.go. It's kept on toskar/swarm/sw1/agent-2",
		"The change is on toskar/swarm/sw1/merged, and the checks passed. It's applied to /work as uncommitted changes.",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("standard error:\n%s\nwant:\n%s", errOut, strings.Join(want, "\n"))
	}
	if !strings.Contains(out, "Merged answer") || strings.Contains(out, "Agent 1 ·") {
		t.Errorf("standard output should have the answer alone: %q", out)
	}
	// Always, for an agent, is that agent's chat alone.
	if len(f.decisions) != 1 || f.decisions[0] != "req-a2 allow in this chat" {
		t.Errorf("decisions %v", f.decisions)
	}
	if f.sent[0].Swarm != "race" {
		t.Errorf("sent %+v", f.sent[0])
	}
}

// A swarm that ran as one agent says why once, in the answer's notice,
// rather than again from its event.
func TestChatSwarmNote(t *testing.T) {
	f := newFakeChat()
	f.msgs["0000000a"] = []contracts.Message{{Role: "assistant", Content: "Answer 1",
		Meta: &contracts.MessageMeta{Notice: "One agent: a race needs at least two models to compare."}}}
	go func() {
		payload := map[string]any{"conversation_id": "0000000a", "reason": "oneModel", "note": "One agent: a race needs at least two models to compare."}
		f.events <- api.Event{Type: "swarm.note", Payload: &payload}
	}()
	time.Sleep(50 * time.Millisecond)
	out, errOut, err := runChat(t, f, []string{"--swarm", "race"}, "hello\n/exit\n", true)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(out+errOut, "One agent: a race needs at least two models"); n != 1 {
		t.Errorf("the note shows %d times:\n%s\n%s", n, out, errOut)
	}
}

// --swarm sends swarm on each message; /swarm on the next one only; and
// /swarm off stops what --swarm asked for.
func TestChatSwarmFlags(t *testing.T) {
	f := newFakeChat()
	if _, errOut, err := runChat(t, f, nil, "/swarm race\none\ntwo\n/swarm\nthree\n/swarm nope\n/exit\n", true); err != nil {
		t.Fatal(err)
	} else if !strings.Contains(errOut, `/swarm takes split, race, or off, not "nope"`) {
		t.Errorf("stderr %q", errOut)
	}
	got := []string{}
	for _, r := range f.sent {
		got = append(got, r.Swarm)
	}
	if strings.Join(got, ",") != "race,,split" {
		t.Errorf("swarms sent: %q", got)
	}

	f = newFakeChat()
	if _, _, err := runChat(t, f, []string{"--swarm", "race", "first"}, "second\n/swarm off\nthird\n/exit\n", true); err != nil {
		t.Fatal(err)
	}
	got = nil
	for _, r := range f.sent {
		got = append(got, r.Swarm)
	}
	if strings.Join(got, ",") != "race,race," || f.sent[0].Message != "first" {
		t.Errorf("swarms sent: %q", got)
	}

	if _, _, err := runChat(t, newFakeChat(), []string{"--swarm=all"}, "", true); err == nil || !strings.Contains(err.Error(), "--swarm is split or race") {
		t.Errorf("a mode that isn't one: %v", err)
	}
}

func TestSwarmArg(t *testing.T) {
	for _, c := range []struct {
		args       []string
		rest, mode string
	}{
		{[]string{"--swarm", "fix the tests"}, "fix the tests", "split"},
		{[]string{"--swarm", "race", "fix it"}, "fix it", "race"},
		{[]string{"-swarm=split", "--model", "m"}, "--model m", "split"},
		{[]string{"--model", "m", "hello"}, "--model m hello", ""},
		{[]string{"--", "--swarm"}, "-- --swarm", ""},
	} {
		rest, mode, err := swarmArg(c.args)
		if err != nil || strings.Join(rest, " ") != c.rest || mode != c.mode {
			t.Errorf("%v: %v %q %v", c.args, rest, mode, err)
		}
	}
}

// /agents lists the last swarm's agents; /open N continues one's chat,
// without a swarm of its own, and /back returns to the swarm's chat.
func TestChatSwarmAgentsOpenBack(t *testing.T) {
	f := newSwarmFake()
	out, errOut := runSwarmChat(t, f, []string{"--swarm"},
		"fix the sums\ny\n/agents\n/open 2\nand edge cases?\n/back\nthanks\n/open 9\n/back\n/exit\n", false)
	for _, want := range []string{"1. code · done  agent-1", "2. tests · done  agent-2", "Toskar: I wrote sum_test.go."} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in:\n%s", want, out)
		}
	}
	for _, want := range []string{"the last swarm has no agent 9", "you're not in an agent's chat"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("no %q in:\n%s", want, errOut)
		}
	}
	if len(f.sent) != 3 {
		t.Fatalf("sent %d", len(f.sent))
	}
	if *f.sent[1].ConversationId != "agent-2" || f.sent[1].Swarm != "" {
		t.Errorf("in the agent's chat: %+v", f.sent[1])
	}
	if *f.sent[2].ConversationId != *f.sent[0].ConversationId || f.sent[2].Swarm != "split" {
		t.Errorf("back in the swarm's chat: %+v", f.sent[2])
	}
}

// /agents finds a resumed chat's swarm from the chat, and says when there's none.
func TestChatAgentsFromTheChat(t *testing.T) {
	f := newFakeChat()
	f.convs = []contracts.Conversation{{ID: "11111111", SwarmID: "sw9"}, {ID: "22222222"}}
	f.swarms = map[string]contracts.Swarm{"sw9": {ID: "sw9", Agents: []contracts.SwarmAgent{{Agent: 1, ConversationID: "c1", Part: "Read the docs", Status: "out_of_budget"}}}}
	out, _, err := runChat(t, f, []string{"--resume", "11111111"}, "/agents\n/exit\n", true)
	if err != nil || !strings.Contains(out, "1. Read the docs · out of budget  c1") {
		t.Errorf("%v\n%s", err, out)
	}
	_, errOut, _ := runChat(t, f, []string{"--resume", "22222222"}, "/agents\n/exit\n", true)
	if !strings.Contains(errOut, "no swarm has run in this chat yet") {
		t.Errorf("stderr %q", errOut)
	}
}

// The swarm goes in POST /chat's body, beside what the SDK sends.
func TestSwarmField(t *testing.T) {
	bodies := make(chan map[string]any, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := map[string]any{}
		_ = json.Unmarshal(raw, &body)
		bodies <- body
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: done\ndata: {}\n\n")
	}))
	defer srv.Close()
	sdk, err := toskar.New(toskar.WithURL(srv.URL), toskar.WithHTTP(&http.Client{Transport: swarmField{next: http.DefaultTransport}}))
	if err != nil {
		t.Fatal(err)
	}
	d := daemonChat{sdk: sdk}
	for _, mode := range []string{"race", ""} {
		for _, err := range d.stream(context.Background(), chatRequest{ChatJSONRequestBody: api.ChatJSONRequestBody{Message: "hi"}, Swarm: mode}) {
			if err != nil {
				t.Fatal(err)
			}
		}
		body := <-bodies
		if body["message"] != "hi" || body["stream"] != true {
			t.Errorf("body %v", body)
		}
		if got, has := body["swarm"]; (mode == "" && has) || (mode != "" && got != mode) {
			t.Errorf("swarm %q: body %v", mode, body)
		}
	}
}
