package muninn

import (
	"encoding/json"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	toskar "github.com/yeixio/toskar-core"
)

// Memory commands in the other languages (#718): "Merk dir, dass …",
// "…を覚えておいて", "记住：…". English keeps its own patterns above, with
// typo correction; each other language's words are in
// i18n/memory/<language>.json, which the phone app reads too.

// commandWordList is one language's memory command words.
type commandWordList struct {
	// Spaced is false for Japanese, Korean, and Chinese, whose phrases
	// match without word boundaries.
	Spaced bool `json:"spaced"`
	// Polite words may come first: "bitte", "por favor".
	Polite   []string      `json:"polite"`
	Remember phraseWords   `json:"remember"`
	Forget   phraseWords   `json:"forget"`
	List     []string      `json:"list"`
	Nothing  []string      `json:"nothing"`
	code     string        `json:"-"`
	patterns *commandRegex `json:"-"`
}

// phraseWords are a command's phrases before the text ("merk dir"), what
// may join them to it ("dass"), and phrases after it ("merk dir das").
type phraseWords struct {
	Before  []string `json:"before"`
	Joiners []string `json:"joiners"`
	After   []string `json:"after"`
}

type commandRegex struct {
	rememberBefore, rememberAfter, forgetBefore, forgetAfter, list *regexp.Regexp
}

var (
	commandListsOnce sync.Once
	commandLists     []*commandWordList
)

// otherLanguages is every language's memory command words but English's.
func otherLanguages() []*commandWordList {
	commandListsOnce.Do(func() {
		files, _ := fs.Glob(toskar.Catalog, "i18n/memory/*.json")
		sort.Strings(files)
		for _, file := range files {
			raw, err := fs.ReadFile(toskar.Catalog, file)
			if err != nil {
				continue
			}
			var w commandWordList
			if json.Unmarshal(raw, &w) != nil {
				continue
			}
			w.code = strings.TrimSuffix(path.Base(file), ".json")
			if w.code == "en" {
				continue
			}
			w.patterns = w.compile()
			commandLists = append(commandLists, &w)
		}
	})
	return commandLists
}

// alternation is the phrases as a regular expression alternation, longest
// first, so "merk dir das" wins over "merk dir".
func alternation(phrases []string) string {
	sorted := append([]string(nil), phrases...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return utf8.RuneCountInString(sorted[i]) > utf8.RuneCountInString(sorted[j])
	})
	quoted := make([]string, 0, len(sorted))
	for _, p := range sorted {
		if p = strings.TrimSpace(p); p != "" {
			quoted = append(quoted, strings.ReplaceAll(regexp.QuoteMeta(p), " ", `\s+`))
		}
	}
	return "(?:" + strings.Join(quoted, "|") + ")"
}

func (w *commandWordList) compile() *commandRegex {
	polite := ""
	if len(w.Polite) > 0 {
		polite = `(?:` + alternation(w.Polite) + `[,\s]+)?`
	}
	before := func(p phraseWords) *regexp.Regexp {
		if len(p.Before) == 0 {
			return nil
		}
		if !w.Spaced {
			joiner := ""
			if len(p.Joiners) > 0 {
				joiner = alternation(p.Joiners) + `?`
			}
			return regexp.MustCompile(`(?s)^\s*` + alternation(p.Before) + `\s*` + joiner + `\s*(.+?)\s*[。.!！]*$`)
		}
		// A joiner ending in an apostrophe ("qu'") runs into the text;
		// anything else is followed by a space or a mark, so "ricordare la
		// spesa" isn't "ricorda" plus "re la spesa".
		var glued, spaced []string
		for _, j := range p.Joiners {
			if strings.HasSuffix(j, "'") {
				glued = append(glued, j)
			} else {
				spaced = append(spaced, j)
			}
		}
		join := `[\s,:]+`
		if len(spaced) > 0 {
			join = `(?:[\s,:]+` + alternation(spaced) + `)?[\s,:]+`
		}
		if len(glued) > 0 {
			join = `(?:[\s,:]+` + alternation(glued) + `\s*|` + join + `)`
		}
		return regexp.MustCompile(`(?is)^\s*` + polite + alternation(p.Before) + join + `(.+?)\s*$`)
	}
	after := func(p phraseWords) *regexp.Regexp {
		if len(p.After) == 0 {
			return nil
		}
		if !w.Spaced {
			return regexp.MustCompile(`(?s)^\s*(.+?)\s*[、,，]?\s*` + alternation(p.After) + `\s*[。.!！]*$`)
		}
		// A comma, period, or dash comes before it, as in English.
		return regexp.MustCompile(`(?is)^\s*(.+?)\s*[,.;:!—–-]+\s*` + polite + alternation(p.After) + `[\s.!]*$`)
	}
	c := &commandRegex{
		rememberBefore: before(w.Remember),
		rememberAfter:  after(w.Remember),
		forgetBefore:   before(w.Forget),
		forgetAfter:    after(w.Forget),
	}
	if len(w.List) > 0 {
		end := `(?:[\s?？.!。！,]|$)`
		if !w.Spaced {
			end = ``
		}
		c.list = regexp.MustCompile(`(?is)^\s*` + polite + alternation(w.List) + end)
	}
	return c
}

// parseOther reads a memory command in one of the other languages.
// Remembering is checked before forgetting, so "Vergiss nicht, dass …"
// (don't forget that) isn't "Vergiss …" (forget).
func parseOther(msg string) Command {
	// One apostrophe, so "n’oublie" is "n'oublie"; and a Spanish question
	// or exclamation starts after its opening mark.
	msg = strings.TrimLeft(strings.TrimSpace(strings.ReplaceAll(msg, "’", "'")), "¿¡")
	for _, w := range otherLanguages() {
		p := w.patterns
		if p.list != nil && p.list.MatchString(msg) {
			return Command{Kind: CommandList}
		}
		for _, re := range []*regexp.Regexp{p.rememberBefore, p.rememberAfter} {
			if re == nil {
				continue
			}
			if m := re.FindStringSubmatch(msg); m != nil {
				if text := tidy(m[1]); w.memory(text) {
					return Command{Kind: CommandRemember, Text: text}
				}
			}
		}
		for _, re := range []*regexp.Regexp{p.forgetBefore, p.forgetAfter} {
			if re == nil {
				continue
			}
			if m := re.FindStringSubmatch(msg); m != nil {
				if text := tidy(m[1]); w.memory(text) {
					return Command{Kind: CommandForget, Text: text}
				}
			}
		}
	}
	return Command{}
}

// questionEnd ends a question, which isn't a memory: "记住了吗", "覚えてる？".
var questionEnd = regexp.MustCompile(`(?:[?？]|吗|嗎|か|까)\s*$`)

// memory reports whether text is something to remember, not "that" or a
// question.
func (w *commandWordList) memory(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" || questionEnd.MatchString(t) {
		return false
	}
	for _, n := range w.Nothing {
		if t == strings.ToLower(n) {
			return false
		}
	}
	if !w.Spaced {
		// 忘记密码？ is "Forgot your password?", not a request.
		return utf8.RuneCountInString(t) >= 2 && !strings.ContainsAny(t, "?？")
	}
	return len(strings.Fields(t)) >= 1
}
