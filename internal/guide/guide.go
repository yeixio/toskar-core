// Package guide finds the parts of the user guide a question about
// Yggdrasil needs, so the assistant answers from Yggdrasil's own
// documentation instead of guessing.
package guide

import (
	"encoding/json"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	userguide "github.com/yeixio/toskar-core/docs/user-guide"
)

// Passage is one paragraph or list of steps from a section.
type Passage struct {
	Section string
	Text    string
}

type indexed struct {
	Passage
	terms map[string]int
	n     int
}

// corpus is passages with the word weights BM25 ranks them by.
type corpus struct {
	passages []indexed
	idf      map[string]float64
	avgLen   float64
	// maxIDF is the rarest word's weight (see coverage).
	maxIDF float64
}

var (
	loadOnce sync.Once
	// overview is each section's paragraphs and steps, which every version
	// of the guide has. details is a detailed page's body (#1222): its
	// prose, steps, lists, tables, and notes, asked only when the overview
	// has no answer, so a page's detail never outranks a section's own
	// overview.
	overview, details corpus
)

func (c *corpus) add(section, text string) {
	// The section title counts toward every passage in it.
	terms := map[string]int{}
	n := 0
	for _, t := range tokens(section + " " + section + " " + text) {
		terms[t]++
		n++
	}
	c.passages = append(c.passages, indexed{Passage: Passage{Section: section, Text: text}, terms: terms, n: n})
}

// weigh sets the word weights once the passages are in.
func (c *corpus) weigh() {
	df := map[string]int{}
	total := 0
	for _, p := range c.passages {
		total += p.n
		for t := range p.terms {
			df[t]++
		}
	}
	c.idf = map[string]float64{}
	for t, d := range df {
		c.idf[t] = math.Log(1 + (float64(len(c.passages))-float64(d)+0.5)/(float64(d)+0.5))
		c.maxIDF = max(c.maxIDF, c.idf[t])
	}
	if len(c.passages) > 0 {
		c.avgLen = float64(total) / float64(len(c.passages))
	}
}

func numbered(items []string) string {
	var b strings.Builder
	for i, st := range items {
		b.WriteString(strconv.Itoa(i+1) + ". " + st + "\n")
	}
	return strings.TrimSpace(b.String())
}

func load() {
	var g struct {
		Sections []struct {
			Title      string   `json:"title"`
			Paragraphs []string `json:"paragraphs"`
			Steps      []string `json:"steps"`
			Blocks     []struct {
				Type  string     `json:"type"`
				Text  string     `json:"text"`
				Title string     `json:"title"`
				Items []string   `json:"items"`
				Rows  [][]string `json:"rows"`
			} `json:"blocks"`
		} `json:"sections"`
	}
	if json.Unmarshal(userguide.GuideJSON, &g) != nil {
		return
	}
	for _, s := range g.Sections {
		for _, p := range s.Paragraphs {
			overview.add(s.Title, p)
		}
		if len(s.Steps) > 0 {
			overview.add(s.Title, numbered(s.Steps))
		}
		for _, b := range s.Blocks {
			switch b.Type {
			case "paragraph", "note":
				details.add(s.Title, b.Text)
			case "steps":
				details.add(s.Title, numbered(b.Items))
			case "list":
				text := "- " + strings.Join(b.Items, "\n- ")
				if b.Title != "" {
					text = b.Title + ":\n" + text
				}
				details.add(s.Title, text)
			case "table":
				rows := make([]string, len(b.Rows))
				for i, r := range b.Rows {
					rows[i] = strings.Join(r, ": ")
				}
				details.add(s.Title, strings.Join(rows, "\n"))
			}
		}
	}
	overview.weigh()
	details.weigh()
}

var stop = map[string]bool{}

func init() {
	// The last line is the product's name, and the one from before the
	// rename (#237): it says a question is about the app (selfRe), not which
	// part of the guide answers it.
	for _, w := range strings.Fields(`a an the and or but if then of to in on at for from by with without about into over
		is are was were be been being am do does did done can could would should will shall may might must
		i me my mine you your yours we our us it its this that these those there here what which who whom whose
		how why where when not no yes so as than too very just also only own same such get got make made
		have has had having please tell show explain want need like use using used way thing things
		toskar yggdrasil`) {
		stop[w] = true
	}
}

// tokens are a text's lower-case words, without common words, with a
// plural or -ing ending trimmed so "automations" finds "automation".
func tokens(s string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len(w) < 2 || stop[w] {
			continue
		}
		switch {
		case strings.HasSuffix(w, "ies") && len(w) > 4:
			w = w[:len(w)-3] + "y"
		case strings.HasSuffix(w, "ing") && len(w) > 5:
			w = w[:len(w)-3]
		case strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") && len(w) > 3:
			w = w[:len(w)-1]
		}
		out = append(out, w)
	}
	return out
}

// scored is a passage and how well it matches a question.
type scored struct {
	p     indexed
	score float64
}

// rank scores every passage for a question's words (BM25), best first,
// keeping those that score at least min.
func (c *corpus) rank(q map[string]bool, min float64) []scored {
	loadOnce.Do(load)
	var all []scored
	const k1, b = 1.2, 0.75
	for _, p := range c.passages {
		s := 0.0
		for t := range q {
			f := float64(p.terms[t])
			if f == 0 {
				continue
			}
			s += c.idf[t] * f * (k1 + 1) / (f + k1*(1-b+b*float64(p.n)/c.avgLen))
		}
		if s >= min {
			all = append(all, scored{p, s})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].score > all[j].score })
	return all
}

func termSet(question string) map[string]bool {
	q := map[string]bool{}
	for _, t := range tokens(question) {
		q[t] = true
	}
	return q
}

// Search returns up to max passages for a question, best first, that score
// at least min (see About).
func Search(question string, max int, min float64) []Passage {
	all := overview.rank(termSet(question), min)
	out := make([]Passage, 0, max)
	for i := 0; i < len(all) && i < max; i++ {
		out = append(out, all[i].p.Passage)
	}
	return out
}

// coverage is the share of a question's words, weighted by how rare they
// are, that a passage has. A word the guide never uses counts as the rarest:
// "17 sheep", "Maya", and "muffins" say a question isn't about Toskar,
// however many common words it shares with a passage.
func (c *corpus) coverage(q map[string]bool, p indexed) float64 {
	loadOnce.Do(load)
	total, matched := 0.0, 0.0
	for t := range q {
		w, ok := c.idf[t]
		if !ok {
			w = c.maxIDF
		}
		total += w
		if p.terms[t] > 0 {
			matched += w
		}
	}
	if total == 0 {
		return 0
	}
	return matched / total
}

var (
	// questionRe is a message asking something.
	questionRe = regexp.MustCompile(`(?i)\?|^\s*(how|what|where|why|when|which|can|could|does|do|is|are|will|explain|tell me|show me|help)\b`)
	// selfRe is a message about Toskar itself, under its name or the one
	// from before the rename (#237).
	selfRe = regexp.MustCompile(`(?i)\b(toskar|yggdrasil|this app|the app|your (own )?(features?|docs?|documentation|settings|capabilit\w*|guide)|(do|does|can|will|would|what) you (support|offer|have|do)|in the (app|ui|settings)|what left this computer|this computer only|tool sources?|connected services?|specialized ais?|join (token|command)s?|paired computers?|team profile` +
		// Privacy questions are about this app: "are my chats private?",
		// "who can see my data?", "do I need encryption?".
		`|my (data|chats?|conversations?|files|memories|messages|information) (is |are )?(private|encrypted|safe|secure|stored)` +
		`|(see|read|access) my (data|chats?|conversations?|files|memories|messages)|do i need encryption|is encryption (required|needed))\b`)
)

// Thresholds: a question naming Yggdrasil needs a modest match; one that
// doesn't, a strong match on the guide's own words, with most of its words
// in the best passage (minCoverage). A long question that shares a few
// common words with the guide ("take one every half hour", "how many
// months") scores high on BM25 alone; coverage tells it from "How do I
// schedule an automation?".
const (
	minNamed    = 2.0
	minStrong   = 6.0
	minCoverage = 0.6
	maxChars    = 3000
)

// About returns the guide passages for a question about Yggdrasil, or
// none for anything else.
func About(message string) []Passage {
	m := strings.TrimSpace(message)
	if !questionRe.MatchString(m) || len(strings.Fields(m)) > 60 {
		return nil
	}
	q := termSet(m)
	named := selfRe.MatchString(m)
	min := minStrong
	if named {
		min = minNamed
	}
	ranked := overview.found(q, min, named)
	if len(ranked) == 0 {
		// A detailed page's body answers what no overview does, such as
		// what a role in the profile editor is for (#1222).
		ranked = details.found(q, min, named)
	}
	if len(ranked) == 0 {
		return nil
	}
	var found []Passage
	for i := 0; i < len(ranked) && i < 4; i++ {
		found = append(found, ranked[i].p.Passage)
		if i == 0 {
			// The best section's detailed page, if it has one, goes on
			// from its overview: "what does Never discusses do?" finds
			// Profiles, then the Topics table on its page (#1222).
			found = append(found, details.within(q, ranked[0].p.Section, ranked[0].p.Text, 2)...)
		}
	}
	var out []Passage
	size := 0
	for _, p := range found {
		if size+len(p.Text) > maxChars && len(out) > 0 {
			break
		}
		out = append(out, p)
		size += len(p.Text)
	}
	return out
}

// within is up to max of a section's passages that share a word with the
// question, best first, other than skip.
func (c *corpus) within(q map[string]bool, section, skip string, max int) []Passage {
	var out []Passage
	for _, r := range c.rank(q, 0) {
		if len(out) == max {
			break
		}
		if r.p.Section == section && r.p.Text != skip && r.score > 0 {
			out = append(out, r.p.Passage)
		}
	}
	return out
}

// found is the passages that answer the question, best first, or none:
// the best must score min and, unless the question names Toskar, have
// most of its words.
func (c *corpus) found(q map[string]bool, min float64, named bool) []scored {
	ranked := c.rank(q, min)
	if len(ranked) == 0 || (!named && c.coverage(q, ranked[0].p) < minCoverage) {
		return nil
	}
	return ranked
}

// Block is passages as instructions for the model.
func Block(ps []Passage) string {
	if len(ps) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("From Toskar's user guide, for a question about Toskar itself. Answer from these excerpts and name screens and buttons as they say; if they don't cover the question, say so instead of guessing:\n")
	for _, p := range ps {
		b.WriteString("\n[" + p.Section + "]\n" + p.Text + "\n")
	}
	return strings.TrimSpace(b.String())
}
