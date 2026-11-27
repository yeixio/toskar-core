package app

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// While a tool runs, voice mode says it's looking: in the answer's
// language, fitting the tool, and not the same words twice in a row (#1487).
func TestVoiceFillers(t *testing.T) {
	c := &voiceChat{}
	first := c.filler("en", "internet.search")
	if !strings.Contains(strings.ToLower(first), "look") && !strings.Contains(strings.ToLower(first), "check") {
		t.Fatalf("a web search says %q", first)
	}
	if again := c.filler("en", "internet.search"); again == first {
		t.Fatalf("said %q twice in a row", again)
	}
	if es := c.filler("es-MX", "places.search"); !strings.Contains(es, "momento") && !strings.Contains(es, "busc") {
		t.Fatalf("in Spanish: %q", es)
	}
	if got := c.filler("sv", "filesystem.read"); got == "" || !isASCII(got) {
		t.Fatalf("a language without fillers falls back to English: %q", got)
	}
	for lang, set := range voiceFillers {
		for _, list := range [][]string{set.web, set.data, set.other, set.wait} {
			if len(list) == 0 {
				t.Fatalf("%s has an empty list", lang)
			}
			for _, s := range list {
				// Each is a sentence of its own, so it's spoken at once.
				if !strings.ContainsAny(s[len(s)-3:], ".。") {
					t.Fatalf("%s %q doesn't end a sentence", lang, s)
				}
			}
		}
	}
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}

// A slow answer has a "just a sec" or two while it starts, then the answer;
// one quicker than the first filler has none (#1487).
func TestVoiceWaitFillers(t *testing.T) {
	t.Setenv("TOSKAR_STUB_INFERENCE", "1")
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	a, err := New(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })
	first, next := firstWaitFiller, nextWaitFiller
	t.Cleanup(func() { firstWaitFiller, nextWaitFiller = first, next })
	slow := time.Duration(0)
	a.StubReply = func(string, []pluginapi.ChatMessage) string {
		time.Sleep(slow)
		return "Otters hold hands while they sleep."
	}
	ctx := context.Background()
	chat := &voiceChat{app: a, ctx: ctx}
	ask := func() string {
		var said strings.Builder
		if err := chat.Respond(ctx, "Tell me about otters.", func(s string) { said.WriteString(s) }); err != nil {
			t.Fatal(err)
		}
		<-chat.saving
		return said.String()
	}
	// Quicker than the first filler's time.
	firstWaitFiller = time.Minute
	if got := ask(); got != "Otters hold hands while they sleep." {
		t.Fatalf("a quick answer: %q", got)
	}
	firstWaitFiller, nextWaitFiller = 40*time.Millisecond, 60*time.Millisecond
	slow = 400 * time.Millisecond
	got := ask()
	answer := strings.Index(got, "Otters")
	if answer < 0 {
		t.Fatalf("no answer: %q", got)
	}
	before := got[:answer]
	if !strings.Contains(before, voiceFillers["en"].wait[0]) {
		t.Fatalf("a slow answer: %q", got)
	}
	if n := strings.Count(before, ". "); n > maxFillers {
		t.Fatalf("%d fillers: %q", n, got)
	}
	time.Sleep(150 * time.Millisecond)
	if strings.Contains(got[answer:], "sec") {
		t.Fatalf("a filler after the answer started: %q", got)
	}
}
