package huginn

import (
	"strings"
	"testing"

	"github.com/yeixio/yggdrasil-core/pkg/contracts"
)

func TestClassify(t *testing.T) {
	cases := map[string]Kind{
		"hi there":                                      Chat,
		"What is DNS?":                                  Chat,
		"What's my zip code format in Canada?":          Chat,
		"I take a yoga class on Mondays, any tips?":     Chat,
		"What's the weather in Denver?":                 Current,
		"Who won the game last night?":                  Current,
		"What is the latest news about the Mars rover?": Current,
		"Summarize https://example.com/post":            Current,
		"Fix the bug in my Python script":               Coding,
		"Why does `x := foo()` fail to compile?":        Coding,
		"Write a function that reverses a string":       Coding,
		"What's wrong with my C++ loop":                 Coding,
		"Compare the pros and cons of three NAS drives": Research,
		"Make a plan for moving to a new city":          Research,
		"Run the tests in this repo":                    Local,
		"What files are in my Downloads folder?":        Local,
		"List my files in this folder":                  Local,
		"How do I use underscore in names?":             Chat,
		"":                                              Chat,
	}
	for msg, want := range cases {
		if got := Classify(msg); got != want {
			t.Errorf("Classify(%q) = %s, want %s", msg, got, want)
		}
	}
	if got := Classify(strings.Repeat("Tell me a story about a fox. ", 30)); got != Research {
		t.Errorf("a long request = %s, want research", got)
	}
}

const gb = 1_000_000_000

func model(id string, mem uint64, tags, purpose []string, caps contracts.ModelCapabilities) contracts.Model {
	return contracts.Model{ID: id, DisplayName: id, Installed: true, MemoryNeeded: mem, Tags: tags, Purpose: purpose, Capabilities: caps}
}

var (
	tiny    = model("llama-1b", 1.6*gb, []string{"general", "fast"}, []string{"assistant", "general"}, contracts.ModelCapabilities{ToolCalling: true})
	mid     = model("qwen-7b", 6.4*gb, []string{"general"}, []string{"assistant", "general"}, contracts.ModelCapabilities{ToolCalling: true})
	coder   = model("coder-7b", 6.4*gb, []string{"coding"}, []string{"coding"}, contracts.ModelCapabilities{ToolCalling: true, Coding: true})
	big     = model("qwen-14b", 12*gb, []string{"general", "large", "reasoning"}, []string{"assistant", "general", "research"}, contracts.ModelCapabilities{ToolCalling: true})
	gemma   = model("gemma-9b", 8*gb, []string{"general", "large"}, []string{"assistant", "general"}, contracts.ModelCapabilities{})
	r1      = model("r1-7b", 6.4*gb, []string{"reasoning"}, []string{"research"}, contracts.ModelCapabilities{})
	vision  = model("llava-7b", 7*gb, []string{"vision", "general"}, []string{"assistant", "general"}, contracts.ModelCapabilities{})
	library = []contracts.Model{tiny, mid, coder, big, gemma, r1, vision}
)

func TestChooseMatchesTheRequest(t *testing.T) {
	const ram = 24 * gb // quick ≤ 9.6 GB, full ≤ 14.4 GB
	cases := []struct {
		kind Kind
		want string
	}{
		{Chat, "gemma-9b"},     // largest conversational model that loads fast
		{Coding, "coder-7b"},   // the coding model
		{Research, "qwen-14b"}, // largest that fits
		{Current, "qwen-14b"},  // must call tools; gemma cannot
		{Local, "qwen-14b"},
	}
	for _, c := range cases {
		got, ok := Choose(c.kind, library, ram)
		if !ok || got.Model.ID != c.want {
			t.Errorf("Choose(%s) = %s, want %s", c.kind, got.Model.ID, c.want)
		}
		if !strings.Contains(got.Reason, c.kind.Describe()) || !strings.Contains(got.Reason, got.Model.ID) {
			t.Errorf("reason = %q", got.Reason)
		}
	}
}

func TestChooseQuickQuestionUsesTheLoadedModel(t *testing.T) {
	loaded := mid
	loaded.Status = "running"
	got, _ := Choose(Chat, []contracts.Model{tiny, loaded, gemma}, 24*gb)
	if got.Model.ID != "qwen-7b" {
		t.Fatalf("got %s, want the running qwen-7b", got.Model.ID)
	}
}

func TestChooseRespectsMemory(t *testing.T) {
	got, _ := Choose(Research, library, 8*gb) // full ≤ 4.8 GB
	if got.Model.ID != "llama-1b" {
		t.Fatalf("got %s on an 8 GB computer", got.Model.ID)
	}
	// Nothing fits: the smallest model is still better than no answer.
	got, ok := Choose(Chat, []contracts.Model{big, mid}, 4*gb)
	if !ok || got.Model.ID != "qwen-7b" {
		t.Fatalf("got %s %v", got.Model.ID, ok)
	}
	if _, ok := Choose(Chat, nil, 4*gb); ok {
		t.Fatal("no installed model must report false")
	}
}

func TestFallback(t *testing.T) {
	got, ok := Fallback("qwen-14b", library, 24*gb)
	if !ok || got.ID != "gemma-9b" {
		t.Fatalf("fallback from 14b = %s", got.ID)
	}
	if !Smaller(big, got) {
		t.Fatal("9b after 14b should be called out")
	}
	got, ok = Fallback("llama-1b", []contracts.Model{tiny, mid}, 24*gb)
	if !ok || got.ID != "qwen-7b" {
		t.Fatalf("fallback from the smallest = %s", got.ID)
	}
	if _, ok := Fallback("llama-1b", []contracts.Model{tiny}, 24*gb); ok {
		t.Fatal("no other model must report false")
	}
}

func TestSmallAndLarger(t *testing.T) {
	for p, want := range map[string]float64{"1B": 1, "3.8B": 3.8, "500M": 0.5, "14b": 14} {
		if got, ok := Billions(contracts.Model{Parameters: p}); !ok || got != want {
			t.Errorf("Billions(%q) = %v %v", p, got, ok)
		}
	}
	if _, ok := Billions(contracts.Model{Parameters: "large"}); ok {
		t.Error("an unreadable size is unknown")
	}
	a, b := tiny, mid
	a.Parameters, b.Parameters = "1B", "7B"
	if !Small(a) || Small(b) || Small(contracts.Model{}) {
		t.Fatal("small")
	}
	if got, ok := Larger([]contracts.Model{a, b}, 24*gb); !ok || got.ID != "qwen-7b" {
		t.Fatalf("larger = %v %v", got.ID, ok)
	}
	if _, ok := Larger([]contracts.Model{a}, 24*gb); ok {
		t.Fatal("nothing larger")
	}
}
