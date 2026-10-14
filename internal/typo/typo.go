// Package typo corrects near-miss words in a message toward the words a rule
// matches, so "whats teh wether tomorow" counts as a weather question and
// "remmeber that i like tea" as a memory (#451). The models cope with typos;
// Toskar's own rules match exact words, so they need the help.
//
// A rule's vocabulary comes from its own regular expressions: every literal
// word of four letters or more, so it can't drift from what the rule
// matches. A word in a message is corrected only when it isn't already a
// vocabulary word or a common English word, isn't a name, and is close to
// exactly one vocabulary word: one edit for words of 4 to 7 letters, two for
// 8 or more, none for 3 or fewer. Swapping two neighbouring letters ("teh")
// is one edit.
//
// The phone app keeps a copy of this in TypeScript (mobile/src/typo.ts in
// yeixio/toskar-apps); tests/quality/text.json holds both to the same
// answers.
package typo

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Vocab is the words a set of rules matches.
type Vocab struct {
	words map[string]bool
	list  []string
	// folded is each word without accents, so "manana" is "mañana".
	folded map[string]string
}

var (
	// escapeRe is a regex escape such as \b or \s, and flagRe a group's
	// flags such as (?i): neither is a word.
	escapeRe = regexp.MustCompile(`\\[a-zA-Z]`)
	flagRe   = regexp.MustCompile(`\(\?[a-zA-Z]+\)`)
	wordRe   = regexp.MustCompile(`[a-zA-Z']+`)
)

// FromPatterns collects the vocabulary of regular expressions, given as
// their sources (Regexp.String). A word joined to a group or class, such as
// "analy" in analy(s|z)e, is part of a longer word and left out.
func FromPatterns(patterns ...string) *Vocab {
	v := &Vocab{words: map[string]bool{}}
	for _, p := range patterns {
		p = flagRe.ReplaceAllString(escapeRe.ReplaceAllString(p, " "), " ")
		for _, loc := range wordRe.FindAllStringIndex(p, -1) {
			if loc[1] < len(p) && strings.ContainsRune("([", rune(p[loc[1]])) {
				continue
			}
			if loc[0] > 0 && strings.ContainsRune(")]", rune(p[loc[0]-1])) {
				continue
			}
			v.add(p[loc[0]:loc[1]])
		}
	}
	sort.Strings(v.list)
	return v
}

// FromWords collects a vocabulary from words and phrases, such as a
// language's automation request words (internal/automations).
func FromWords(phrases ...string) *Vocab {
	v := &Vocab{words: map[string]bool{}}
	for _, p := range phrases {
		for _, w := range tokenRe.FindAllString(p, -1) {
			v.add(w)
		}
	}
	sort.Strings(v.list)
	return v
}

func (v *Vocab) add(w string) {
	w = strings.ToLower(strings.Trim(w, "'"))
	if utf8.RuneCountInString(w) < 4 || v.words[w] {
		return
	}
	v.words[w] = true
	v.list = append(v.list, w)
	if f := fold(w); f != w {
		if v.folded == nil {
			v.folded = map[string]string{}
		}
		v.folded[f] = w
	}
}

// fold takes the accents off letters: "mañana" is "manana", "émission"
// "emission".
func fold(s string) string {
	out, _, err := transform.String(transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC), s)
	if err != nil {
		return s
	}
	return out
}

// Words lists the vocabulary, for tests.
func (v *Vocab) Words() []string { return append([]string(nil), v.list...) }

// tokenRe is a word in a message: letters and apostrophes.
var tokenRe = regexp.MustCompile(`[\p{L}']+`)

// Fix corrects the message's near-miss words toward the vocabulary.
func (v *Vocab) Fix(message string) string { return v.fix(message, -1) }

// FixLeading corrects only the message's first n words, such as a command
// ("remmeber that …") whose rest is the person's own text.
func (v *Vocab) FixLeading(message string, n int) string { return v.fix(message, n) }

func (v *Vocab) fix(message string, n int) string {
	if v == nil || len(v.list) == 0 {
		return message
	}
	seen := 0
	first := true
	return tokenRe.ReplaceAllStringFunc(message, func(token string) string {
		seen++
		atStart := first
		first = false
		if n >= 0 && seen > n {
			return token
		}
		if w := v.correct(token, atStart); w != "" {
			return matchCase(w, token)
		}
		return token
	})
}

// correct is the vocabulary word token was meant to be, or "".
func (v *Vocab) correct(token string, atStart bool) string {
	word := strings.ToLower(strings.Trim(token, "'"))
	size := utf8.RuneCountInString(word)
	limit := 1
	switch {
	case size <= 3:
		return ""
	case size >= 8:
		limit = 2
	}
	if v.words[word] || common[word] || inflected(word, v.words) {
		return ""
	}
	// Typed without its accents: "manana" is "mañana".
	if w, ok := v.folded[word]; ok {
		return w
	}
	// A capitalized word past the start is likely a name: "Brandy", "Lisa".
	if !atStart {
		if r, _ := utf8.DecodeRuneInString(token); unicode.IsUpper(r) {
			return ""
		}
	}
	// The closest word wins. Between two as close, one of the same length
	// does ("thansk" is "thanks" with two letters swapped, not "thank" with
	// one added); otherwise there's no telling which was meant.
	best, bestDist, bestSame, tie := "", limit+1, false, false
	for _, cand := range v.list {
		n := utf8.RuneCountInString(cand)
		if abs(n-size) > limit {
			continue
		}
		d, same := Distance(fold(word), fold(cand), limit), n == size
		if d > limit {
			continue
		}
		switch {
		case d < bestDist, d == bestDist && same && !bestSame:
			best, bestDist, bestSame, tie = cand, d, same, false
		case d == bestDist && same == bestSame && cand != best:
			tie = true
		}
	}
	if best == "" || tie {
		return ""
	}
	return best
}

// suffixes are endings that make another form of a word: "tests" and "test"
// are both real, and neither is a typo of the other.
var suffixes = []string{"s", "es", "d", "ed", "ing", "er", "ly", "'s"}

// inflected reports a word that is a vocabulary word with an ending added or
// taken off.
func inflected(word string, words map[string]bool) bool {
	for _, s := range suffixes {
		if strings.HasSuffix(word, s) && words[strings.TrimSuffix(word, s)] {
			return true
		}
		if words[word+s] {
			return true
		}
	}
	return false
}

// matchCase gives w the capitalization of token: all caps, a capital first
// letter, or lowercase.
func matchCase(w, token string) string {
	if strings.ToUpper(token) == token && strings.ToLower(token) != token {
		return strings.ToUpper(w)
	}
	if r, _ := utf8.DecodeRuneInString(token); unicode.IsUpper(r) {
		r, size := utf8.DecodeRuneInString(w)
		return string(unicode.ToUpper(r)) + w[size:]
	}
	return w
}

// Distance is the optimal string alignment distance between a and b: the
// edits (insert, delete, substitute, or swap two neighbouring letters) that
// turn one into the other. It stops counting past limit and returns limit+1.
func Distance(a, b string, limit int) int {
	ra, rb := []rune(a), []rune(b)
	if abs(len(ra)-len(rb)) > limit {
		return limit + 1
	}
	prev2 := make([]int, len(rb)+1)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		rowMin := cur[0]
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				cur[j] = min(cur[j], prev2[j-2]+1)
			}
			rowMin = min(rowMin, cur[j])
		}
		if rowMin > limit {
			return limit + 1
		}
		prev2, prev, cur = prev, cur, prev2
	}
	return min(prev[len(rb)], limit+1)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
