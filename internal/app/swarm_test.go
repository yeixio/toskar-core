package app

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/swarm"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// swarmApp is an app with stub inference whose planner splits every request
// in two, whose agents answer by their number, and whose coordinator answers
// "Merged." Agents named in hold wait until released or stopped.
type swarmApp struct {
	*App
	mu      sync.Mutex
	final   string
	hold    map[int]chan struct{}
	started map[int]chan struct{}
	once    map[int]*sync.Once
}

func newSwarmApp(t *testing.T, hold ...int) *swarmApp {
	t.Helper()
	t.Setenv("TOSKAR_STUB_INFERENCE", "1")
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	a, err := New(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })
	// Two slots, so two agents on this computer's one model run at once.
	old := swarm.SlotsFor
	swarm.SlotsFor = func(string, string) int { return 2 }
	t.Cleanup(func() { swarm.SlotsFor = old })
	s := &swarmApp{App: a, hold: map[int]chan struct{}{}, started: map[int]chan struct{}{}, once: map[int]*sync.Once{}}
	for _, n := range hold {
		s.hold[n], s.started[n], s.once[n] = make(chan struct{}), make(chan struct{}), &sync.Once{}
	}
	a.StubReply = func(_ string, messages []pluginapi.ChatMessage) string {
		system, last := messages[0].Content, messages[len(messages)-1].Content
		switch {
		case strings.Contains(system, "Split the request"):
			return `{"parts": [{"title": "Prices", "instructions": "Compare the prices."}, {"title": "Reviews", "instructions": "Sum up the reviews."}]}`
		case strings.Contains(last, "You are agent 1 of"):
			s.wait(1)
			return "Agent one's answer."
		case strings.Contains(last, "You are agent 2 of"):
			s.wait(2)
			return "Agent two's answer."
		}
		s.mu.Lock()
		s.final = last
		s.mu.Unlock()
		return "Merged."
	}
	return s
}

// wait holds agent n's reply. An agent's text can reach the model more than
// once in a turn, so only the first call says it started.
func (s *swarmApp) wait(n int) {
	if hold, ok := s.hold[n]; ok {
		s.once[n].Do(func() { close(s.started[n]) })
		select {
		case <-hold:
		case <-time.After(10 * time.Second):
		}
	}
}

// ask runs a swarm turn in a new chat and returns its answer and the chat.
func (s *swarmApp) ask(t *testing.T, ctx context.Context, mode string, during func(chat string)) (string, contracts.Conversation) {
	t.Helper()
	// A profile without tools, so nothing looks anything up on the web.
	quiet, err := s.Profiles.Create(ctx, profiles.Profile{Name: "Quiet", OrchestratorID: "simple", Roles: []contracts.ModelRole{{Role: "assistant"}}})
	if err != nil {
		t.Fatal(err)
	}
	conv, err := s.Conversations.Create(ctx, "Biology", quiet.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := s.RunChat(swarm.WithMode(ctx, mode), "", conv.ID, "Explain how photosynthesis works and how respiration works.", true, "", "")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan string)
	go func() {
		var answer string
		for c := range stream {
			answer += c.Content
		}
		done <- answer
	}()
	if during != nil {
		during(conv.ID)
	}
	select {
	case answer := <-done:
		conv, _ = s.Conversations.Get(ctx, conv.ID)
		return answer, conv
	case <-time.After(20 * time.Second):
		t.Fatal("the swarm didn't finish")
	}
	return "", conv
}

// agentChat waits for agent n's chat in the person's only swarm.
func (s *swarmApp) agentChat(t *testing.T, ctx context.Context, n int) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if list, _ := s.Swarms.List(ctx, ""); len(list) == 1 && len(list[0].Agents) >= n {
			return list[0].Agents[n-1].ConversationID
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no agent chat")
	return ""
}

// A split's agents are real chats in a project made for the job, with their
// part and answer, and the coordinator answers from their whole answers.
func TestSwarmAgentsAreChats(t *testing.T) {
	s := newSwarmApp(t)
	ctx := context.Background()
	answer, coord := s.ask(t, ctx, swarm.ModeSplit, nil)
	if answer != "Merged." {
		t.Fatalf("answer = %q", answer)
	}
	if !strings.Contains(s.final, "Agent one's answer.") || !strings.Contains(s.final, "Agent two's answer.") {
		t.Fatalf("the coordinator's prompt lacks the agents' answers:\n%s", s.final)
	}
	list, _ := s.Swarms.List(ctx, "")
	if len(list) != 1 {
		t.Fatalf("swarms = %+v", list)
	}
	sw := list[0]
	if sw.Status != swarm.StatusDone || sw.ConversationID != coord.ID || len(sw.Agents) != 2 || sw.ProjectID == "" {
		t.Fatalf("swarm = %+v", sw)
	}
	if coord.ProjectID != sw.ProjectID || coord.SwarmID != sw.ID || coord.SwarmRole != "" {
		t.Fatalf("the coordinator chat = %+v", coord)
	}
	if p, err := s.Projects.Get(ctx, sw.ProjectID); err != nil || !strings.HasPrefix(p.Name, "Explain how photosynthesis") {
		t.Fatalf("the job's project = %+v, %v", p, err)
	}
	for i, ag := range sw.Agents {
		if ag.Status != swarm.AgentDone || ag.Summary == "" {
			t.Errorf("agent %d = %+v", i+1, ag)
		}
		chat, err := s.Conversations.Get(ctx, ag.ConversationID)
		if err != nil || chat.SwarmID != sw.ID || chat.SwarmRole != swarm.AgentRole(i+1) || chat.ProjectID != sw.ProjectID {
			t.Fatalf("agent %d's chat = %+v, %v", i+1, chat, err)
		}
		msgs, _ := s.Conversations.ListMessages(ctx, ag.ConversationID)
		if len(msgs) != 2 || !strings.Contains(msgs[0].Content, "photosynthesis") || msgs[1].Role != "assistant" {
			t.Fatalf("agent %d's messages = %+v", i+1, msgs)
		}
	}
}

// Stopping one agent's chat stops that agent, and the merge goes on with the
// rest.
func TestSwarmStopOneAgent(t *testing.T) {
	s := newSwarmApp(t, 2)
	ctx := context.Background()
	answer, _ := s.ask(t, ctx, swarm.ModeSplit, func(string) {
		chat := s.agentChat(t, ctx, 2)
		<-s.started[2]
		if !s.StopChat(chat) {
			t.Error("agent 2's turn wasn't running")
		}
		close(s.hold[2])
	})
	if answer != "Merged." {
		t.Fatalf("answer = %q", answer)
	}
	if !strings.Contains(s.final, "Agent one's answer.") || strings.Contains(s.final, "Agent two's answer.") {
		t.Fatalf("the merge used a stopped agent, or not the other:\n%s", s.final)
	}
	list, _ := s.Swarms.List(ctx, "")
	if got := list[0].Agents; got[0].Status != swarm.AgentDone || got[1].Status != swarm.AgentStopped || list[0].Status != swarm.StatusDone {
		t.Fatalf("swarm = %+v", list[0])
	}
}

// Stopping the coordinator's chat stops every agent.
func TestSwarmStopAll(t *testing.T) {
	s := newSwarmApp(t, 1, 2)
	ctx := context.Background()
	s.ask(t, ctx, swarm.ModeSplit, func(coord string) {
		s.agentChat(t, ctx, 2)
		<-s.started[1]
		<-s.started[2]
		if !s.StopChat(coord) {
			t.Error("the coordinator's turn wasn't running")
		}
		close(s.hold[1])
		close(s.hold[2])
	})
	list, _ := s.Swarms.List(ctx, "")
	if list[0].Status != swarm.StatusStopped {
		t.Fatalf("swarm = %+v", list[0])
	}
	for i, ag := range list[0].Agents {
		if ag.Status != swarm.AgentStopped {
			t.Errorf("agent %d = %s", i+1, ag.Status)
		}
	}
}

// One computer and one model with one slot runs as one agent and says why.
func TestSwarmOneAtATime(t *testing.T) {
	s := newSwarmApp(t)
	swarm.SlotsFor = func(string, string) int { return 1 }
	ctx := context.Background()
	answer, coord := s.ask(t, ctx, swarm.ModeSplit, nil)
	if answer != "Merged." {
		t.Fatalf("answer = %q", answer)
	}
	list, _ := s.Swarms.List(ctx, "")
	if len(list) != 1 || list[0].Note != swarm.Reason(swarm.OneAtATime) || len(list[0].Agents) != 0 || list[0].Status != swarm.StatusDone {
		t.Fatalf("swarm = %+v", list)
	}
	msgs, _ := s.Conversations.ListMessages(ctx, coord.ID)
	last := msgs[len(msgs)-1]
	if last.Meta == nil || last.Meta.Notice != swarm.Reason(swarm.OneAtATime) {
		t.Fatalf("no notice under the answer: %+v", last.Meta)
	}
	if coord.ProjectID != "" {
		t.Errorf("a project was made for one agent: %q", coord.ProjectID)
	}
}
