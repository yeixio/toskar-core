package huginn

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Issue is something in an answer that does not check out (spec §24).
type Issue struct {
	// Kind is "arithmetic" for a calculation that does not add up, or
	// "unsupported" for a figure the sources do not contain.
	Kind string
	// Text is the figure or calculation, as written.
	Text string
	// Want is the correct result of a calculation.
	Want string
}

var (
	numberRe = regexp.MustCompile(`\$?\d(?:[\d,]*\d)?(?:\.\d+)?`)
	// dateRe finds dates, whose numbers are not figures to check.
	dateRe = regexp.MustCompile(`(?i)\b(?:(?:jan|feb|mar|apr|may|jun|jul|aug|sep|sept|oct|nov|dec)[a-z]*\.?\s+\d{1,2}(?:st|nd|rd|th)?(?:,?\s+\d{4})?|\d{1,2}(?:st|nd|rd|th)?\s+(?:of\s+)?(?:jan|feb|mar|apr|may|jun|jul|aug|sep|sept|oct|nov|dec)[a-z]*\.?(?:,?\s+\d{4})?|\d{4}-\d{2}-\d{2}|\d{1,2}/\d{1,2}/\d{2,4})\b`)
	// calcRe finds "a op b = c" in an answer.
	calcRe = regexp.MustCompile(`(\$?\d[\d,]*(?:\.\d+)?)\s*([+\-−×x*/÷])\s*(\$?\d[\d,]*(?:\.\d+)?)\s*(?:=|≈|is)\s*(\$?\d[\d,]*(?:\.\d+)?)`)
)

func parseNumber(s string) (float64, bool) {
	s = strings.ReplaceAll(strings.TrimPrefix(s, "$"), ",", "")
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil
}

// near reports whether two figures match once rounding is allowed for:
// 52.5 matches 52.50 and 52, and 1,249 matches 1249.
func near(a, b float64) bool {
	if a == b {
		return true
	}
	diff := math.Abs(a - b)
	if diff < 0.006 {
		return true
	}
	// Whole-number rounding, and 1% for larger figures.
	return diff <= 0.5 && math.Abs(b) >= 10 || diff <= math.Abs(b)*0.01
}

func numbers(text string) []float64 {
	var out []float64
	for _, m := range numberRe.FindAllString(text, -1) {
		if v, ok := parseNumber(m); ok {
			out = append(out, v)
		}
	}
	return out
}

// worthChecking skips figures that are rarely claims about the sources: small
// counts, list numbers, and years.
func worthChecking(raw string, v float64) bool {
	if strings.Contains(raw, ".") || strings.HasPrefix(raw, "$") {
		return true
	}
	if v >= 1900 && v <= 2100 && !strings.Contains(raw, ",") {
		return false
	}
	return v >= 10
}

// derivable reports whether v can be reached from two source figures with one
// operation, such as a price per terabyte or a total.
func derivable(v float64, src []float64) bool {
	const limit = 80
	if len(src) > limit {
		src = src[:limit]
	}
	for i, a := range src {
		for j, b := range src {
			if i == j {
				continue
			}
			if near(v, a+b) || near(v, a-b) || near(v, a*b) || (b != 0 && near(v, a/b)) {
				return true
			}
		}
	}
	return false
}

// Check looks for calculations that do not add up and, when sources are
// given, figures the sources do not support. It runs without a model, so it
// is cheap enough to run on every answer that used data; only an answer with
// issues gets a second model pass.
func Check(answer, evidence, question string) []Issue {
	var issues []Issue
	seen := map[string]bool{}
	computed := map[string]bool{}
	for _, m := range calcRe.FindAllStringSubmatch(answer, -1) {
		a, okA := parseNumber(m[1])
		b, okB := parseNumber(m[3])
		got, okC := parseNumber(m[4])
		if !okA || !okB || !okC {
			continue
		}
		var want float64
		switch m[2] {
		case "+":
			want = a + b
		case "-", "−":
			want = a - b
		case "×", "x", "*":
			want = a * b
		case "/", "÷":
			if b == 0 {
				continue
			}
			want = a / b
		}
		computed[m[4]] = true
		if !near(got, want) {
			issues = append(issues, Issue{Kind: "arithmetic", Text: strings.TrimSpace(m[0]), Want: strconv.FormatFloat(math.Round(want*100)/100, 'f', -1, 64)})
			seen[m[4]] = true
		}
	}
	if strings.TrimSpace(evidence) == "" {
		return issues
	}
	lines := evidenceLines(evidence)
	asked := numbers(question)
	for _, sentence := range sentenceRe.Split(dateRe.ReplaceAllString(answer, " "), -1) {
		// A figure must come from the lines about what the sentence is
		// about: "20 in stock" next to "Michelin Defender" is checked
		// against that product's row, not against every number loaded.
		src := append(numbers(strings.Join(relevantLines(sentence, lines), "\n")), asked...)
		for _, raw := range numberRe.FindAllString(sentence, -1) {
			v, ok := parseNumber(raw)
			if !ok || seen[raw] || computed[raw] || !worthChecking(raw, v) {
				continue
			}
			seen[raw] = true
			grounded := false
			for _, s := range src {
				if near(v, s) {
					grounded = true
					break
				}
			}
			if !grounded && !derivable(v, src) {
				issues = append(issues, Issue{Kind: "unsupported", Text: raw})
			}
		}
	}
	return issues
}

var (
	sentenceRe = regexp.MustCompile(`[.!?]\s+|\n+`)
	wordRe     = regexp.MustCompile(`[\p{L}][\p{L}\p{N}'-]{2,}`)
)

func evidenceLines(evidence string) []string {
	var out []string
	for _, l := range strings.Split(evidence, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func words(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range wordRe.FindAllString(strings.ToLower(s), -1) {
		out[w] = true
	}
	return out
}

// relevantLines returns the evidence lines that share the most words with a
// sentence, when at least two words match; otherwise all lines, so a
// sentence that names nothing is still checked against everything.
func relevantLines(sentence string, lines []string) []string {
	want := words(sentence)
	best, bestScore := []string(nil), 0
	for _, l := range lines {
		score := 0
		for w := range words(l) {
			if want[w] {
				score++
			}
		}
		switch {
		case score > bestScore:
			best, bestScore = []string{l}, score
		case score == bestScore && score > 0:
			best = append(best, l)
		}
	}
	if bestScore < 2 {
		return lines
	}
	return best
}

// Describe lists issues for the model that will correct the answer.
func Describe(issues []Issue) string {
	var b strings.Builder
	for _, is := range issues {
		switch is.Kind {
		case "arithmetic":
			b.WriteString("- The calculation \"" + is.Text + "\" is wrong; the result is " + is.Want + ".\n")
		default:
			b.WriteString("- " + is.Text + " does not appear in the reference material.\n")
		}
	}
	return b.String()
}

// Figures lists the issues' figures for the user, such as "20 and $176.50".
func Figures(issues []Issue) string {
	var parts []string
	for _, is := range issues {
		parts = append(parts, is.Text)
	}
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	default:
		return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
	}
}

// HasFigures reports whether an answer states figures worth checking.
func HasFigures(answer string) bool {
	for _, raw := range numberRe.FindAllString(answer, -1) {
		if v, ok := parseNumber(raw); ok && worthChecking(raw, v) {
			return true
		}
	}
	return calcRe.MatchString(answer)
}
