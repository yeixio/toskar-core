package inventory

import (
	"strings"
	"testing"
)

func snapshot() Snapshot {
	return Snapshot{
		Models: []Model{
			{ID: "llama-3.2-1b-q4", Name: "Llama 3.2 1B", ToolCalling: true, MemoryNeeded: 2 << 30, On: []string{"This Mac"}},
			{ID: "qwen2.5-14b-q4", Name: "Qwen 2.5 14B", MemoryNeeded: 12 << 30, On: []string{"Studio"}},
		},
		Nodes: []Node{
			{ID: "here", Name: "This Mac", Local: true, Online: true, MemoryBytes: 8 << 30, Trainer: "MLX"},
			{ID: "studio", Name: "Studio", Online: true, MemoryBytes: 64 << 30},
			{ID: "old", Name: "Old PC", Online: false, MemoryBytes: 16 << 30},
		},
		Tools: []Tool{
			{ID: "internet.search", Name: "Web Search", Source: "builtin", Enabled: true},
			{ID: "terminal", Name: "Terminal", Source: "builtin", Enabled: true},
			{ID: "files.create", Name: "Create File", Source: "builtin", Enabled: true},
			{ID: "git.status", Name: "Git Status", Source: "builtin", Enabled: false},
			{ID: "mail.send", Name: "Send Gmail message", Description: "Send an email from Gmail", Source: "mcp:gmail", Enabled: true},
		},
		Connectors: []Connector{{ID: "github", Name: "GitHub", Connected: true}, {ID: "homeassistant", Name: "Home Assistant"}},
	}
}

func ability(t *testing.T, list []Ability, id string) Ability {
	t.Helper()
	for _, a := range list {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("no ability %s", id)
	return Ability{}
}

func TestAbilities(t *testing.T) {
	all := Abilities(snapshot())
	for id, want := range map[string]bool{
		"web": true, "run_code": true, "make_files": true, "git": false, "email": true, "github": true,
		"home": false, "image_generation": false, "vision": false, "training": true, "other_computers": true, "meaning_search": false,
	} {
		if a := ability(t, all, id); a.Available != want {
			t.Errorf("%s = %+v", id, a)
		}
	}
	if a := ability(t, all, "email"); a.Via[0] != "Send Gmail message" {
		t.Errorf("email via = %v", a.Via)
	}
	if a := ability(t, all, "image_generation"); !strings.Contains(a.Note, "tool source") {
		t.Errorf("image note = %q", a.Note)
	}
}

func TestCapabilityQuestions(t *testing.T) {
	s := snapshot()
	for q, want := range map[string]string{
		"Can you generate an image of a cat?": "image_generation",
		"Can you execute Python?":             "run_code",
		"Do you have access to my email?":     "email",
		"Can you search the web?":             "web",
		"Are you able to use GitHub?":         "github",
	} {
		got := Ask(s, q)
		if len(got) == 0 || got[0].ID != want {
			t.Errorf("%q = %+v, want %s", q, got, want)
		}
	}
	for _, q := range []string{"Generate an image of a cat", "What is the capital of France?", "Run the tests"} {
		if got := Ask(s, q); len(got) != 0 {
			t.Errorf("%q treated as a capability question: %+v", q, got)
		}
	}
	facts := Facts(s, "Can you generate an image?")
	if !strings.Contains(facts, "Generate images: no. No image model") || !strings.Contains(facts, "do not claim abilities") {
		t.Errorf("facts = %q", facts)
	}
}

func TestWhichComputerCanRunAModel(t *testing.T) {
	s := snapshot()
	places, ok := NodesFor(s, "qwen2.5-14b-q4")
	if !ok || len(places) != 3 {
		t.Fatalf("places = %+v", places)
	}
	if places[0].Node != "Studio" || places[0].Note != "can run it now" {
		t.Errorf("best = %+v", places[0])
	}
	for _, p := range places {
		if p.Node == "This Mac" && (p.Fits || !strings.Contains(p.Note, "needs about 12.0 GB")) {
			t.Errorf("this mac = %+v", p)
		}
		if p.Node == "Old PC" && p.Note != "offline" {
			t.Errorf("old = %+v", p)
		}
	}
	if _, ok := NodesFor(s, "nope"); ok {
		t.Error("unknown model")
	}
	facts := Facts(s, "Which computer can run Qwen 2.5 14B?")
	if !strings.Contains(facts, "Qwen 2.5 14B on Studio: can run it now") {
		t.Errorf("facts = %q", facts)
	}
}
