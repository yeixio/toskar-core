package runlog

import (
	"testing"
	"time"
)

func turn(chat string, at time.Time, model string, prompt, cached int) Run {
	return Run{ConversationID: chat, Status: StatusCompleted, StartedAt: at,
		Models: []ModelUse{{ModelID: model, Backend: "metal", PromptTokens: prompt, CachedTokens: cached}}}
}

func TestSummarizePromptCache(t *testing.T) {
	now := time.Now()
	at := func(m int) time.Time { return now.Add(time.Duration(m) * time.Minute) }
	runs := []Run{
		// A chat's first turn starts cold, and isn't counted.
		turn("a", at(0), "llama", 400, 20),
		turn("a", at(1), "llama", 450, 400),
		turn("a", at(2), "llama", 500, 450),
		// Newest first, as Since returns them: the order inside a chat is by time.
		turn("b", at(9), "gemma", 900, 0),
		turn("b", at(8), "gemma", 800, 100),
		turn("b", at(7), "gemma", 700, 50),
		turn("b", at(6), "gemma", 600, 0),
		turn("b", at(5), "gemma", 500, 0),
		turn("b", at(4), "gemma", 400, 20),
		{ConversationID: "a", Status: StatusFailed, StartedAt: at(3), Models: []ModelUse{{ModelID: "llama", Backend: "metal", PromptTokens: 9}}},
		turn("", at(3), "llama", 100, 0), // not in a chat
	}
	// Just loaded: cold, not counted.
	loaded := turn("a", at(4), "llama", 600, 0)
	loaded.Models[0].LoadMs = 900
	// Another runtime or computer, which doesn't report its cache.
	other := turn("a", at(5), "ollama-model", 600, 0)
	other.Models[0].Backend = ""
	runs = append(runs, loaded, other)

	got := SummarizePromptCache(now, runs).Models
	if len(got) != 2 {
		t.Fatalf("models = %+v", got)
	}
	gemma, llama := got[0], got[1]
	if gemma.ModelID != "gemma" || gemma.Turns != 5 || gemma.PromptTokens != 3500 || gemma.CachedTokens != 150 || !gemma.Low {
		t.Errorf("gemma = %+v", gemma)
	}
	if llama.ModelID != "llama" || llama.Turns != 2 || llama.CachedTokens != 850 || llama.Low {
		t.Errorf("llama = %+v", llama)
	}
	if want := 850.0 / 950; llama.Reuse != want {
		t.Errorf("llama reuse = %v, want %v", llama.Reuse, want)
	}
	// Two turns are too few to call it low, however little they reused.
	few := SummarizePromptCache(now, []Run{turn("c", at(0), "x", 10, 0), turn("c", at(1), "x", 10, 0), turn("c", at(2), "x", 10, 0)})
	if m := few.Models[0]; m.Turns != 2 || m.Low {
		t.Errorf("few = %+v", m)
	}
	if empty := SummarizePromptCache(now, nil); empty.Models == nil || len(empty.Models) != 0 {
		t.Errorf("empty = %+v", empty)
	}
}
