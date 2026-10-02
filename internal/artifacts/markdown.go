package artifacts

import (
	"regexp"
	"strings"
)

// The Markdown the assistant writes is turned into a document's blocks:
// headings, paragraphs, lists, code, tables, and rules. Documents and PDFs
// are made from these (Gungnir §21).

type blockKind int

const (
	blockParagraph blockKind = iota
	blockHeading
	blockBullet
	blockNumbered
	blockCode
	blockTable
	blockRule
)

// span is a run of text with one style.
type span struct {
	Text   string
	Bold   bool
	Italic bool
	Code   bool
}

type block struct {
	Kind  blockKind
	Level int // heading level 1–3, or list depth
	Spans []span
	// Number is a numbered list item's number.
	Number int
	// Lines are a code block's lines.
	Lines []string
	// Rows are a table's cells, header first.
	Rows [][][]span
}

var (
	headingRe  = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)
	bulletRe   = regexp.MustCompile(`^(\s*)[-*+]\s+(.*)$`)
	numberedRe = regexp.MustCompile(`^(\s*)(\d+)[.)]\s+(.*)$`)
	ruleRe     = regexp.MustCompile(`^\s*([-*_])(\s*[-*_]){2,}\s*$`)
	tableSepRe = regexp.MustCompile(`^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$`)
)

func parseMarkdown(text string) []block {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var out []block
	var para []string
	flush := func() {
		if len(para) > 0 {
			out = append(out, block{Kind: blockParagraph, Spans: inline(strings.Join(para, " "))})
			para = nil
		}
	}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "```"):
			flush()
			var code []string
			for i++; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```"); i++ {
				code = append(code, strings.TrimRight(lines[i], " \t"))
			}
			out = append(out, block{Kind: blockCode, Lines: code})
		case trimmed == "":
			flush()
		case headingRe.MatchString(trimmed):
			flush()
			m := headingRe.FindStringSubmatch(trimmed)
			level := min(len(m[1]), 3)
			out = append(out, block{Kind: blockHeading, Level: level, Spans: inline(m[2])})
		case ruleRe.MatchString(trimmed):
			flush()
			out = append(out, block{Kind: blockRule})
		case strings.HasPrefix(trimmed, "|") && i+1 < len(lines) && tableSepRe.MatchString(lines[i+1]):
			flush()
			rows := [][][]span{cells(trimmed)}
			for i += 2; i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|"); i++ {
				rows = append(rows, cells(strings.TrimSpace(lines[i])))
			}
			i--
			out = append(out, block{Kind: blockTable, Rows: rows})
		case bulletRe.MatchString(line):
			flush()
			m := bulletRe.FindStringSubmatch(line)
			out = append(out, block{Kind: blockBullet, Level: len(m[1]) / 2, Spans: inline(m[2])})
		case numberedRe.MatchString(line):
			flush()
			m := numberedRe.FindStringSubmatch(line)
			n := 0
			for _, c := range m[2] {
				n = n*10 + int(c-'0')
			}
			out = append(out, block{Kind: blockNumbered, Level: len(m[1]) / 2, Number: n, Spans: inline(m[3])})
		case strings.HasPrefix(trimmed, ">"):
			flush()
			out = append(out, block{Kind: blockParagraph, Level: 1, Spans: inline(strings.TrimSpace(strings.TrimPrefix(trimmed, ">")))})
		default:
			para = append(para, trimmed)
		}
	}
	flush()
	return out
}

func cells(row string) [][]span {
	row = strings.Trim(strings.TrimSpace(row), "|")
	parts := strings.Split(row, "|")
	out := make([][]span, len(parts))
	for i, p := range parts {
		out[i] = inline(strings.TrimSpace(p))
	}
	return out
}

// inline splits text into styled spans: **bold**, *italic* or _italic_,
// and `code`. Links keep their text.
func inline(text string) []span {
	text = linkRe.ReplaceAllString(text, "$1")
	var out []span
	var cur strings.Builder
	bold, italic := false, false
	emit := func(code bool) {
		if cur.Len() > 0 {
			out = append(out, span{Text: cur.String(), Bold: bold, Italic: italic, Code: code})
			cur.Reset()
		}
	}
	r := []rune(text)
	for i := 0; i < len(r); i++ {
		switch {
		case r[i] == '`':
			end := indexRune(r, '`', i+1)
			if end < 0 {
				cur.WriteRune(r[i])
				continue
			}
			emit(false)
			cur.WriteString(string(r[i+1 : end]))
			emit(true)
			i = end
		case (r[i] == '*' || r[i] == '_') && i+1 < len(r) && r[i+1] == r[i]:
			emit(false)
			bold = !bold
			i++
		case r[i] == '*' || (r[i] == '_' && (i == 0 || r[i-1] == ' ' || italic)):
			emit(false)
			italic = !italic
		default:
			cur.WriteRune(r[i])
		}
	}
	emit(false)
	return out
}

var linkRe = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)

func indexRune(r []rune, c rune, from int) int {
	for i := from; i < len(r); i++ {
		if r[i] == c {
			return i
		}
	}
	return -1
}
