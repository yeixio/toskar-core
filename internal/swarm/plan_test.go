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

// A task's parts name the paths they expect to change, cleaned and kept inside
// the folder; a part without paths still reads as before.
func TestPartPaths(t *testing.T) {
	parts := ParseParts(`{"parts": [
		{"title": "API", "instructions": "Add the endpoint.", "paths": ["./internal/api/", "internal/api/handlers.go", "../outside", "/etc/passwd", "."]},
		{"title": "Docs", "instructions": "Document it.", "paths": "docs/api.md"},
		{"title": "Old", "instructions": "No paths."}]}`, 4)
	if len(parts) != 3 {
		t.Fatalf("parts = %+v", parts)
	}
	if got := strings.Join(parts[0].Paths, ","); got != "internal/api,internal/api/handlers.go" {
		t.Errorf("paths = %q", got)
	}
	if got := strings.Join(parts[1].Paths, ","); got != "docs/api.md" {
		t.Errorf("one path as a string = %q", got)
	}
	if parts[2].Paths != nil {
		t.Errorf("no paths = %v", parts[2].Paths)
	}
}

// Parts whose paths overlap, directly or through others, go to one agent;
// parts without paths are left alone.
func TestMergeOverlapping(t *testing.T) {
	parts := MergeOverlapping([]Part{
		{Title: "api", Instructions: "a", Paths: []string{"internal/api"}},
		{Title: "docs", Instructions: "d", Paths: []string{"docs/api.md"}},
		{Title: "handler", Instructions: "h", Paths: []string{"internal/api/handlers.go", "web/client.ts"}},
		{Title: "client", Instructions: "c", Paths: []string{"web/client.ts"}},
		{Title: "loose", Instructions: "l"},
	})
	if len(parts) != 3 {
		t.Fatalf("parts = %+v", parts)
	}
	if parts[0].Title != "api + handler + client" || parts[0].Instructions != "a\n\nh\n\nc" ||
		strings.Join(parts[0].Paths, ",") != "internal/api,internal/api/handlers.go,web/client.ts" {
		t.Errorf("merged = %+v", parts[0])
	}
	if parts[1].Title != "docs" || parts[2].Title != "loose" {
		t.Errorf("the rest = %+v", parts[1:])
	}
}

// A task agent's message gives its part, its paths, and the others', and says
// whether the agents share the folder.
func TestTaskMessage(t *testing.T) {
	parts := []Part{{Title: "api", Instructions: "Add it.", Paths: []string{"api.go"}}, {Title: "docs", Instructions: "Write it.", Paths: []string{"README.md"}}}
	own := TaskMessage(ModeSplit, "Add search.", parts, 1, false)
	for _, want := range []string{"agent 1 of 2", "own copy", "Files you expect to change: api.go", "Agent 2: docs (README.md)", "don't commit"} {
		if !strings.Contains(own, want) {
			t.Errorf("message lacks %q:\n%s", want, own)
		}
	}
	if shared := TaskMessage(ModeSplit, "Add search.", parts, 2, true); !strings.Contains(shared, "take turns in the same project folder") {
		t.Errorf("shared:\n%s", shared)
	}
	if race := TaskMessage(ModeRace, "Add search.", RaceParts("Add search.", 2), 2, false); !strings.Contains(race, "Do the whole task") {
		t.Errorf("race:\n%s", race)
	}
	fix := FixupMessage("Add search.", Agent{N: 1, Part: parts[0]}, Agent{N: 2, Part: parts[1]}, []string{"api.go"})
	for _, want := range []string{"merging agent 2's change", "Agent 1's part (api)", "- api.go", "conflict marker"} {
		if !strings.Contains(fix, want) {
			t.Errorf("fix-up lacks %q:\n%s", want, fix)
		}
	}
}
