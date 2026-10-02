package artifacts

import (
	"bytes"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/encoding/charmap"
)

// MarkdownToPDF makes an A4 PDF from Markdown (Gungnir §21). It uses the
// PDF standard fonts (Helvetica and Courier), so nothing is embedded; text
// outside Western European characters (Windows-1252) is shown as "?".
func MarkdownToPDF(text string) ([]byte, error) {
	p := newPDFLayout()
	for _, b := range parseMarkdown(text) {
		p.block(b)
	}
	return p.finish(), nil
}

const (
	pageW, pageH = 595.0, 842.0
	marginX      = 56.0
	marginTop    = 60.0
	marginBottom = 60.0
	contentW     = pageW - 2*marginX
)

type pdfFont int

const (
	fontRegular pdfFont = iota
	fontBold
	fontItalic
	fontMono
)

var fontNames = []string{"Helvetica", "Helvetica-Bold", "Helvetica-Oblique", "Courier"}

// Character widths, in thousandths of the font size, for ASCII 32–126
// (Adobe's Helvetica and Helvetica-Bold metrics). Other characters use 556.
var helvetica = []int{278, 278, 355, 556, 556, 889, 667, 191, 333, 333, 389, 584, 278, 333, 278, 278, 556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 278, 278, 584, 584, 584, 556, 1015, 667, 667, 722, 722, 667, 611, 778, 722, 278, 500, 667, 556, 833, 722, 778, 667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 278, 278, 278, 469, 556, 333, 556, 556, 500, 556, 556, 278, 556, 556, 222, 222, 500, 222, 833, 556, 556, 556, 556, 333, 500, 278, 556, 500, 722, 500, 500, 500, 334, 260, 334, 584}
var helveticaBold = []int{278, 333, 474, 556, 556, 889, 722, 238, 333, 333, 389, 584, 278, 333, 278, 278, 556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 333, 333, 584, 584, 584, 611, 975, 722, 722, 722, 722, 667, 611, 778, 722, 278, 556, 722, 611, 833, 722, 778, 667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 333, 278, 333, 584, 556, 333, 556, 611, 556, 611, 556, 333, 611, 611, 278, 278, 556, 278, 889, 611, 611, 611, 611, 389, 556, 333, 611, 556, 778, 556, 556, 500, 389, 280, 389, 584}

func charWidth(f pdfFont, b byte) float64 {
	if f == fontMono {
		return 600
	}
	table := helvetica
	if f == fontBold {
		table = helveticaBold
	}
	if b >= 32 && b <= 126 {
		return float64(table[b-32])
	}
	return 556
}

// winAnsi encodes text in the standard fonts' encoding.
func winAnsi(s string) []byte {
	enc := charmap.Windows1252.NewEncoder()
	out := make([]byte, 0, len(s))
	for _, r := range s {
		switch r {
		case '\t':
			out = append(out, ' ', ' ', ' ', ' ')
			continue
		case ' ':
			r = ' '
		}
		if unicode.IsControl(r) {
			continue
		}
		b, err := enc.Bytes([]byte(string(r)))
		if err != nil || len(b) != 1 {
			out = append(out, '?')
			continue
		}
		out = append(out, b[0])
	}
	return out
}

func textWidth(f pdfFont, size float64, b []byte) float64 {
	w := 0.0
	for _, c := range b {
		w += charWidth(f, c)
	}
	return w * size / 1000
}

func spanFont(s span) pdfFont {
	switch {
	case s.Code:
		return fontMono
	case s.Bold:
		return fontBold
	case s.Italic:
		return fontItalic
	}
	return fontRegular
}

type pdfLayout struct {
	pages []*bytes.Buffer
	cur   *bytes.Buffer
	y     float64
}

func newPDFLayout() *pdfLayout {
	p := &pdfLayout{}
	p.newPage()
	return p
}

func (p *pdfLayout) newPage() {
	p.cur = &bytes.Buffer{}
	p.pages = append(p.pages, p.cur)
	p.y = pageH - marginTop
}

// room starts a new page when h does not fit.
func (p *pdfLayout) room(h float64) {
	if p.y-h < marginBottom {
		p.newPage()
	}
}

func pdfString(b []byte) string {
	var s strings.Builder
	s.WriteByte('(')
	for _, c := range b {
		if c == '(' || c == ')' || c == '\\' {
			s.WriteByte('\\')
		}
		s.WriteByte(c)
	}
	s.WriteByte(')')
	return s.String()
}

func (p *pdfLayout) text(x, y float64, f pdfFont, size float64, b []byte) {
	fmt.Fprintf(p.cur, "BT /F%d %.1f Tf %.2f %.2f Td %s Tj ET\n", int(f)+1, size, x, y, pdfString(b))
}

type word struct {
	b     []byte
	font  pdfFont
	space bool // a space comes before it
}

// words splits spans into words that keep their font.
func words(spans []span, base pdfFont) []word {
	var out []word
	space := false
	for _, s := range spans {
		f := spanFont(s)
		if f == fontRegular {
			f = base
		}
		for _, part := range strings.SplitAfter(s.Text, " ") {
			if part == "" {
				continue
			}
			trimmed := strings.TrimRight(part, " ")
			if trimmed != "" {
				out = append(out, word{b: winAnsi(trimmed), font: f, space: space})
			}
			space = strings.HasSuffix(part, " ")
		}
	}
	return out
}

// flow writes spans wrapped to width from x, one line every lead points.
func (p *pdfLayout) flow(spans []span, base pdfFont, size, lead, x, width float64) {
	ws := words(spans, base)
	for len(ws) > 0 {
		lineW := 0.0
		n := 0
		for n < len(ws) {
			w := textWidth(ws[n].font, size, ws[n].b)
			if n > 0 && ws[n].space {
				w += textWidth(ws[n].font, size, []byte{' '})
			}
			if n > 0 && lineW+w > width {
				break
			}
			lineW += w
			n++
		}
		p.room(lead)
		p.y -= lead
		// Words in the same font go out as one string with their spaces,
		// so the text can be copied and searched.
		cx := x
		var run []byte
		runFont, runX := ws[0].font, x
		for i, w := range ws[:n] {
			b := w.b
			// A single word wider than the line is cut to fit.
			for textWidth(w.font, size, b) > width && len(b) > 1 {
				b = b[:len(b)-1]
			}
			if i > 0 && w.space {
				// The space goes with the words before it, in their font.
				run = append(run, ' ')
				cx += textWidth(runFont, size, []byte{' '})
			}
			if i > 0 && w.font != runFont {
				p.text(runX, p.y, runFont, size, run)
				run, runFont, runX = nil, w.font, cx
			}
			run = append(run, b...)
			cx += textWidth(w.font, size, b)
		}
		if len(run) > 0 {
			p.text(runX, p.y, runFont, size, run)
		}
		ws = ws[n:]
	}
}

func (p *pdfLayout) block(b block) {
	switch b.Kind {
	case blockHeading:
		size := map[int]float64{1: 20, 2: 16, 3: 13}[b.Level]
		p.room(size * 2.2)
		p.y -= size * 0.6
		p.flow(b.Spans, fontBold, size, size*1.25, marginX, contentW)
		p.y -= size * 0.3
	case blockParagraph:
		indent := 0.0
		base := fontRegular
		if b.Level > 0 {
			indent, base = 24, fontItalic
		}
		p.flow(b.Spans, base, 11, 15, marginX+indent, contentW-indent)
		p.y -= 6
	case blockBullet, blockNumbered:
		indent := 18 + 18*float64(b.Level)
		marker := "\x95"
		if b.Kind == blockNumbered {
			marker = fmt.Sprintf("%d.", b.Number)
		}
		p.room(15)
		top := p.y
		p.flow(b.Spans, fontRegular, 11, 15, marginX+indent, contentW-indent)
		if top > p.y { // marker on the item's first line
			p.text(marginX+indent-14, top-15, fontRegular, 11, []byte(marker))
		}
		p.y -= 2
	case blockCode:
		for _, line := range b.Lines {
			p.room(12)
			p.y -= 12
			bs := winAnsi(line)
			for textWidth(fontMono, 9.5, bs) > contentW && len(bs) > 1 {
				bs = bs[:len(bs)-1]
			}
			p.text(marginX+8, p.y, fontMono, 9.5, bs)
		}
		p.y -= 8
	case blockRule:
		p.room(14)
		p.y -= 7
		fmt.Fprintf(p.cur, "0.6 G 0.5 w %.2f %.2f m %.2f %.2f l S 0 G\n", marginX, p.y, marginX+contentW, p.y)
		p.y -= 7
	case blockTable:
		p.table(b.Rows)
	}
}

func (p *pdfLayout) table(rows [][][]span) {
	cols := 0
	for _, r := range rows {
		cols = max(cols, len(r))
	}
	if cols == 0 {
		return
	}
	colW := contentW / float64(cols)
	const size, lead, pad = 9.5, 12.5, 4.0
	p.y -= 8
	for i, row := range rows {
		// Lay the row out on a scratch page to learn its height.
		heights := make([]float64, cols)
		for c := 0; c < cols && c < len(row); c++ {
			scratch := &pdfLayout{cur: &bytes.Buffer{}, y: 1e6}
			base := fontRegular
			if i == 0 {
				base = fontBold
			}
			scratch.flow(row[c], base, size, lead, 0, colW-2*pad)
			heights[c] = 1e6 - scratch.y
		}
		h := lead
		for _, x := range heights {
			h = max(h, x)
		}
		h += 2 * pad
		p.room(h)
		top := p.y
		for c := 0; c < cols; c++ {
			x := marginX + float64(c)*colW
			fmt.Fprintf(p.cur, "0.7 G 0.5 w %.2f %.2f %.2f %.2f re S 0 G\n", x, top-h, colW, h)
			if c < len(row) {
				base := fontRegular
				if i == 0 {
					base = fontBold
				}
				p.y = top - pad + 2
				p.flow(row[c], base, size, lead, x+pad, colW-2*pad)
			}
		}
		p.y = top - h
	}
	p.y -= 10
}

// finish writes the PDF file: catalog, page tree, fonts, and each page.
func (p *pdfLayout) finish() []byte {
	var out bytes.Buffer
	var offsets []int
	obj := func(body string) {
		offsets = append(offsets, out.Len())
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", len(offsets), body)
	}
	out.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	nFonts := len(fontNames)
	firstPage := 3 + nFonts
	var kids strings.Builder
	for i := range p.pages {
		fmt.Fprintf(&kids, "%d 0 R ", firstPage+2*i)
	}
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.TrimSpace(kids.String()), len(p.pages)))
	var fonts strings.Builder
	for i, name := range fontNames {
		obj(fmt.Sprintf("<< /Type /Font /Subtype /Type1 /BaseFont /%s /Encoding /WinAnsiEncoding >>", name))
		fmt.Fprintf(&fonts, "/F%d %d 0 R ", i+1, 3+i)
	}
	for i, page := range p.pages {
		// Page number at the foot of each page, when there is more than one.
		if len(p.pages) > 1 {
			num := winAnsi(fmt.Sprintf("%d / %d", i+1, len(p.pages)))
			fmt.Fprintf(page, "0.5 g BT /F1 8.0 Tf %.2f %.2f Td %s Tj ET 0 g\n", pageW/2-textWidth(fontRegular, 8, num)/2, marginBottom/2, pdfString(num))
		}
		obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.0f %.0f] /Resources << /Font << %s>> >> /Contents %d 0 R >>",
			pageW, pageH, fonts.String(), firstPage+2*i+1))
		obj(fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", page.Len(), page.String()))
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, o := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	return out.Bytes()
}
