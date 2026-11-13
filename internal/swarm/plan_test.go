package swarm

import (
	"strings"
	"testing"
)

func TestParseParts(t *testing.T) {
	for name, c := range map[string]struct {
		reply  string
		max    int
		titles []string
	}{
		"objects": {
			reply:  "```json\n{\"parts\": [{\"title\": \"Prices\", \"instructions\": \"Compare the prices.\"}, {\"title\": \"Reviews\", \"instructions\": \"Sum up the reviews.\"}]}\n```",
			max:    4,
			titles: []string{"Prices", "Reviews"},
		},
		"Team's strings": {
			reply:  `{"parts": ["Look up the weather in Juneau", "Look up the ferry times"], "parallel": true}`,
			max:    4,
			titles: []string{"Look up the weather in Juneau", "Look up the ferry times"},
		},
		"capped": {
			reply:  `{"parts": [{"title": "a", "instructions": "do a"}, {"title": "b", "instructions": "do b"}, {"title": "c", "instructions": "do c"}]}`,
			max:    2,
			titles: []string{"a", "b"},
		},
		"empty parts skipped, a title stands in": {
			reply:  `{"parts": [{"title": "", "instructions": ""}, {"title": "Only a title"}]}`,
			max:    4,
			titles: []string{"Only a title"},
		},
		"not JSON": {reply: "I'd split it into prices and reviews.", max: 4},
	} {
		t.Run(name, func(t *testing.T) {
			parts := ParseParts(c.reply, c.max)
			if len(parts) != len(c.titles) {
				t.Fatalf("parts = %+v, want titles %v", parts, c.titles)
			}
			for i, p := range parts {
				if p.Title != c.titles[i] || p.Instructions == "" {
					t.Errorf("part %d = %+v, want title %q", i, p, c.titles[i])
				}
			}
		})
	}
}

// An agent's first message has the whole request, its part, and what the
// others are doing, so it stays in its lane.
func TestFirstMessage(t *testing.T) {
	parts := []Part{{Title: "Prices", Instructions: "Compare the prices."}, {Title: "Reviews", Instructions: "Sum up the reviews."}}
	msg := FirstMessage(ModeSplit, "Which kayak should I buy?", parts, 2)
	for _, want := range []string{"agent 2 of 2", "Which kayak should I buy?", "Sum up the reviews.", "Agent 1: Prices"} {
		if !strings.Contains(msg, want) {
			t.Errorf("first message lacks %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "Agent 2: Reviews") {
		t.Errorf("the agent's own part is listed among the others:\n%s", msg)
	}
	race := FirstMessage(ModeRace, "What's 17 × 23?", RaceParts("What's 17 × 23?", 3), 1)
	if !strings.Contains(race, "agent 1 of 3") || !strings.Contains(race, "What's 17 × 23?") || strings.Contains(race, "Your part") {
		t.Errorf("race message:\n%s", race)
	}
}

func TestNames(t *testing.T) {
	if got := JobName("Compare the three cheapest ferry routes from Juneau to Haines next week, please"); got != "Compare the three cheapest ferry routes from Juneau" {
		t.Errorf("JobName = %q", got)
	}
	if got := JobName("  "); got != "Swarm" {
		t.Errorf("JobName of nothing = %q", got)
	}
	if got := Label(2, "tests"); got != "Agent 2: tests" {
		t.Errorf("Label = %q", got)
	}
	if AgentRole(3) != "agent:3" || AgentNumber("agent:3") != 3 || AgentNumber("worker:3") != 0 || AgentNumber("agent:x") != 0 {
		t.Error("agent roles don't round-trip")
	}
}
