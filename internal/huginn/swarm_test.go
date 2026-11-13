package huginn

import "testing"

func TestAsksForSwarm(t *testing.T) {
	for msg, want := range map[string]int{
		"Use a swarm to compare these three laptops":     0,
		"compare the ferry and the flight, use 3 agents": 3,
		"Plan the garden using four agents.":             4,
		"Research this with a swarm":                     0,
		"USE 2 AGENTS: what's new in llama.cpp?":         2,
	} {
		n, ok := AsksForSwarm(msg)
		if !ok || n != want {
			t.Errorf("AsksForSwarm(%q) = %d, %v; want %d, true", msg, n, ok, want)
		}
	}
	for _, msg := range []string{
		"What is a swarm of bees?",
		"How do agents work?",
		"Tell me about swarming",
		"use one agent",
		"I used 3 agents last time",
		"use a swarm of bees as the example in the poem",
	} {
		if _, ok := AsksForSwarm(msg); ok {
			t.Errorf("AsksForSwarm(%q) = true", msg)
		}
	}
}
