package app

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/mimir"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/swarm"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Each agent of a split finds its own part in the profile's knowledge
// (#1182). Its search was its first message, whose first words are the
// instructions every agent gets, so an agent got another part's file and
// said the reference didn't have its fact.
func TestSwarmAgentsSearchTheirPart(t *testing.T) {
	s := newSwarmApp(t)
	ctx := context.Background()
	var sources []string
	for _, f := range [][2]string{
		{"marathon-winner.txt", "Ana Ruiz won the women's race at the 2026 Juneau Marathon in 2:51:07."},
		{"exchange-rate.txt", "1 US dollar is 0.92 euros."},
	} {
		src, err := s.Mimir.Create(ctx, mimir.CreateInput{Kind: mimir.KindText, Filename: f[0], Text: f[1]})
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, src.ID)
	}
	var mu sync.Mutex
	seen := map[int]string{}
	s.StubReply = func(_ string, messages []pluginapi.ChatMessage) string {
		system, last := messages[0].Content, messages[len(messages)-1].Content
		switch {
		case strings.Contains(system, "Split the request"):
			return `{"parts": [{"title": "Marathon winner", "instructions": "Find who won the women's race at the 2026 Juneau Marathon, and in what time."}, {"title": "Exchange rate", "instructions": "Find the US dollar to euro exchange rate."}]}`
		case strings.Contains(last, "You are agent 1 of"), strings.Contains(last, "You are agent 2 of"):
			n := 1
			if strings.Contains(last, "You are agent 2 of") {
				n = 2
			}
			mu.Lock()
			seen[n] += last
			mu.Unlock()
			return "Agent's answer."
		}
		return "Merged."
	}
	p, err := s.Profiles.Create(ctx, profiles.Profile{Name: "Facts", OrchestratorID: "simple",
		Roles: []contracts.ModelRole{{Role: "assistant"}}, KnowledgeSources: sources})
	if err != nil {
		t.Fatal(err)
	}
	conv, err := s.Conversations.Create(ctx, "Race and rate", p.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := s.RunChat(swarm.WithMode(ctx, swarm.ModeSplit), "", conv.ID,
		"Two things: who won the women's race at the 2026 Juneau Marathon, and in what time? And what's the US dollar to euro exchange rate right now?", true, "", "")
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	go func() {
		for range stream {
		}
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(20 * time.Second):
		t.Fatal("the swarm didn't finish")
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(seen[1], "Ana Ruiz") {
		t.Errorf("agent 1, the marathon, wasn't given its file:\n%s", seen[1])
	}
	if !strings.Contains(seen[2], "0.92") {
		t.Errorf("agent 2, the exchange rate, wasn't given its file:\n%s", seen[2])
	}
	for n, text := range seen {
		// Its part is already one part: it isn't split again.
		if strings.Contains(text, "Notes from earlier parts") || strings.Count(text, "You are agent") != 1 {
			t.Errorf("agent %d worked through its message as a plan:\n%s", n, text)
		}
	}
}

func TestSearchQuery(t *testing.T) {
	parts := []swarm.Part{{Title: "Marathon winner", Instructions: "Find the winner."}, {Title: "Exchange rate", Instructions: "Find the rate."}}
	if got := swarm.SearchQuery(swarm.ModeSplit, "Both, please.", parts, 2); got != "Exchange rate\nFind the rate." {
		t.Errorf("split, agent 2: %q", got)
	}
	if got := swarm.SearchQuery(swarm.ModeRace, "Both, please.", parts, 1); got != "Both, please." {
		t.Errorf("race: %q", got)
	}
	if got := swarm.SearchQuery(swarm.ModeSplit, "Both, please.", parts, 3); got != "Both, please." {
		t.Errorf("no such part: %q", got)
	}
	if got := swarm.SearchQuery(swarm.ModeSplit, "Both, please.", []swarm.Part{{}}, 1); got != "Both, please." {
		t.Errorf("an empty part: %q", got)
	}
}
