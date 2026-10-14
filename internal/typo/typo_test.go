package typo

import (
	"regexp"
	"testing"
)

func TestDistance(t *testing.T) {
	for _, c := range []struct {
		a, b  string
		limit int
		want  int
	}{
		{"weather", "weather", 1, 0},
		{"wether", "weather", 1, 1},   // a letter left out
		{"teh", "the", 1, 1},          // two letters swapped
		{"chekc", "check", 1, 1},      // swapped at the end
		{"tomorow", "tomorrow", 1, 1}, // doubled letter left out
		{"wenesday", "wednesday", 2, 1},
		{"whether", "weather", 1, 2}, // past the limit
		{"cat", "dog", 1, 2},
		{"mañana", "manana", 1, 1},
	} {
		if got := Distance(c.a, c.b, c.limit); got != c.want {
			t.Errorf("Distance(%q, %q, %d) = %d, want %d", c.a, c.b, c.limit, got, c.want)
		}
	}
}

func TestFromPatterns(t *testing.T) {
	v := FromPatterns(`(?i)\b(weather|forecast|analy(s|z)e|news|summari[sz]e|what's new)\b`)
	got := map[string]bool{}
	for _, w := range v.Words() {
		got[w] = true
	}
	for _, w := range []string{"weather", "forecast", "news", "what's"} {
		if !got[w] {
			t.Errorf("%q missing from %v", w, v.Words())
		}
	}
	// Short words, flags, escapes, and stems joined to a group aren't words.
	for _, w := range []string{"analy", "summari", "new", "i", "b"} {
		if got[w] {
			t.Errorf("%q shouldn't be a word: %v", w, v.Words())
		}
	}
}

func TestFix(t *testing.T) {
	v := FromPatterns(regexp.MustCompile(`(?i)\b(weather|forecast|tomorrow|remember|every|morning|check|price|plan|tests?|dates?)\b`).String())
	for _, c := range []struct{ in, want string }{
		{"whats teh wether in juneau tomorow", "whats teh weather in juneau tomorrow"},
		{"remmeber that i like tea", "remember that i like tea"},
		{"evry mornign at 8 chek the price", "every morning at 8 check the price"},
		{"Wether tomorrow?", "Weather tomorrow?"},
		{"FORCAST", "FORECAST"},
		// Real words stay: "whether" is two edits from "weather" anyway,
		// "plain" and "test" are common, "dates" is "date" with an ending.
		{"whether to go", "whether to go"},
		{"a plain plan", "a plain plan"},
		{"a test and the tests", "a test and the tests"},
		{"save the dates", "save the dates"},
		// A capitalized word past the start is a name.
		{"ask Wether about it", "ask Wether about it"},
		// Three letters or fewer are never corrected.
		{"chk the pric", "chk the price"},
	} {
		if got := v.Fix(c.in); got != c.want {
			t.Errorf("Fix(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := v.FixLeading("remmeber my mornign routine", 1); got != "remember my mornign routine" {
		t.Errorf("FixLeading = %q", got)
	}
}

// A word as close to two vocabulary words is left alone: there's no telling
// which was meant.
func TestFixTie(t *testing.T) {
	v := FromPatterns(`\b(bake|bike)\b`)
	if got := v.Fix("bxke"); got != "bxke" {
		t.Errorf("Fix(bxke) = %q, want it unchanged", got)
	}
	// One edit from both, but a swap keeps the length: "thanks", not "thank".
	w := FromPatterns(`^(thanks?|thank you)$`)
	if got := w.Fix("thansk"); got != "thanks" {
		t.Errorf("Fix(thansk) = %q, want thanks", got)
	}
}

func TestFromWordsAndAccents(t *testing.T) {
	v := FromWords("cada mañana", "todos los días", "semana", "every", "tomorrow")
	for _, c := range []struct{ in, want string }{
		{"cada manana a las 8", "cada mañana a las 8"}, // typed without the accent
		{"cada manaña", "cada mañana"},                 // the accent in the wrong place
		{"cada semnaa", "cada semana"},
		{"evry day", "every day"},
		{"todos los dias", "todos los días"},
	} {
		if got := v.Fix(c.in); got != c.want {
			t.Errorf("Fix(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
