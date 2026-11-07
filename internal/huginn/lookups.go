package huginn

import (
	"encoding/json"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	toskar "github.com/yeixio/toskar-core"
	"golang.org/x/text/unicode/norm"
)

// Lookups of what Toskar already keeps, asked for in any of the app's
// languages: the AI news ("read me today's AI news") and the latest results
// of the person's automations ("what's in my briefing"), as said in the car
// (yeixio/toskar-apps#142). Each language's words are in
// i18n/lookups/<language>.json (format in its README.md).

// lookupWordList is one language's lookup words.
type lookupWordList struct {
	Spaced  bool     `json:"spaced"`
	News    []string `json:"news"`
	Results []string `json:"results"`
}

type lookupPatterns struct {
	news, results []*regexp.Regexp
}

var (
	lookupsOnce sync.Once
	lookups     lookupPatterns
)

// lookupWords is every language's patterns, built once.
func lookupWords() lookupPatterns {
	lookupsOnce.Do(func() {
		files, _ := fs.Glob(toskar.Catalog, "i18n/lookups/*.json")
		sort.Strings(files)
		for _, file := range files {
			raw, err := fs.ReadFile(toskar.Catalog, file)
			if err != nil {
				continue
			}
			var w lookupWordList
			if json.Unmarshal(raw, &w) != nil {
				continue
			}
			if re := lookupPattern(w.News, w.Spaced); re != nil {
				lookups.news = append(lookups.news, re)
			}
			if re := lookupPattern(w.Results, w.Spaced); re != nil {
				lookups.results = append(lookups.results, re)
			}
		}
	})
	return lookups
}

// lookupPattern is the phrases as one regular expression, longest first. In
// a spaced language a phrase is whole words; otherwise it matches anywhere,
// with spaces optional.
func lookupPattern(phrases []string, spaced bool) *regexp.Regexp {
	sorted := append([]string(nil), phrases...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return utf8.RuneCountInString(sorted[i]) > utf8.RuneCountInString(sorted[j])
	})
	gap, skip := `\s*`, `.{0,40}`
	if spaced {
		gap, skip = `\s+`, `\s+(?:.{0,40}\s+)?`
	}
	var alts []string
	for _, p := range sorted {
		// … is split off first: NFKC would make it three periods.
		var parts []string
		for _, part := range strings.Split(p, "…") {
			if part = normalizeLookup(part); part != "" {
				parts = append(parts, strings.ReplaceAll(regexp.QuoteMeta(part), " ", gap))
			}
		}
		if len(parts) > 0 {
			alts = append(alts, strings.Join(parts, skip))
		}
	}
	if len(alts) == 0 {
		return nil
	}
	body := "(?:" + strings.Join(alts, "|") + ")"
	if spaced {
		body = `(?:^|[^\pL\pN])` + body + `(?:$|[^\pL\pN])`
	}
	return regexp.MustCompile("(?s)" + body)
}

// normalizeLookup is text as phrases are matched against it: NFKC,
// lowercase, plain apostrophes, single spaces.
func normalizeLookup(text string) string {
	text = strings.ToLower(norm.NFKC.String(text))
	text = strings.NewReplacer("’", "'", "‘", "'").Replace(text)
	return strings.Join(strings.Fields(text), " ")
}

func matchesAny(res []*regexp.Regexp, message string) bool {
	text := normalizeLookup(message)
	for _, re := range res {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

// AsksForNews reports a message asking for the AI news, in any of the
// app's languages: "read me today's AI news", "KI-Nachrichten von heute".
func AsksForNews(message string) bool {
	return matchesAny(lookupWords().news, message)
}

// AsksForResults reports a message asking for the latest results of the
// person's automations, in any of the app's languages: "what's in my
// briefing", "résultats de mes automatisations".
func AsksForResults(message string) bool {
	return matchesAny(lookupWords().results, message)
}
