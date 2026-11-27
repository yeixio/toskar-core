package app

import (
	"strings"
	"testing"
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
		for _, list := range [][]string{set.web, set.data, set.other} {
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
