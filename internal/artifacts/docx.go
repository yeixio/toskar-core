package artifacts

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
)

// MarkdownToDOCX makes a Word document from Markdown (Gungnir §21):
// headings, paragraphs with bold, italic, and code, lists, code blocks,
// tables, and rules.
func MarkdownToDOCX(text string) ([]byte, error) {
	var body strings.Builder
	for _, b := range parseMarkdown(text) {
		switch b.Kind {
		case blockHeading:
			fmt.Fprintf(&body, `<w:p><w:pPr><w:pStyle w:val="Heading%d"/></w:pPr>%s</w:p>`, b.Level, docxRuns(b.Spans))
		case blockParagraph:
			ppr := ""
			if b.Level > 0 {
				ppr = `<w:pPr><w:pStyle w:val="Quote"/></w:pPr>`
			}
			fmt.Fprintf(&body, `<w:p>%s%s</w:p>`, ppr, docxRuns(b.Spans))
		case blockBullet, blockNumbered:
			marker := "•"
			if b.Kind == blockNumbered {
				marker = fmt.Sprintf("%d.", b.Number)
			}
			indent := 360 + 360*b.Level
			// A tab stop at the text's indent puts the text right after the marker.
			fmt.Fprintf(&body, `<w:p><w:pPr><w:pStyle w:val="ListParagraph"/><w:tabs><w:tab w:val="left" w:pos="%d"/></w:tabs><w:ind w:left="%d" w:hanging="360"/></w:pPr><w:r><w:t xml:space="preserve">%s</w:t></w:r><w:r><w:tab/></w:r>%s</w:p>`,
				indent, indent, escape(marker), docxRuns(b.Spans))
		case blockCode:
			for _, line := range b.Lines {
				fmt.Fprintf(&body, `<w:p><w:pPr><w:pStyle w:val="Code"/></w:pPr><w:r><w:t xml:space="preserve">%s</w:t></w:r></w:p>`, escape(line))
			}
		case blockRule:
			body.WriteString(`<w:p><w:pPr><w:pBdr><w:bottom w:val="single" w:sz="6" w:space="1" w:color="999999"/></w:pBdr></w:pPr></w:p>`)
		case blockTable:
			body.WriteString(`<w:tbl><w:tblPr><w:tblStyle w:val="Table"/><w:tblW w:w="5000" w:type="pct"/>` +
				`<w:tblBorders><w:top w:val="single" w:sz="4" w:color="BBBBBB"/><w:left w:val="single" w:sz="4" w:color="BBBBBB"/>` +
				`<w:bottom w:val="single" w:sz="4" w:color="BBBBBB"/><w:right w:val="single" w:sz="4" w:color="BBBBBB"/>` +
				`<w:insideH w:val="single" w:sz="4" w:color="BBBBBB"/><w:insideV w:val="single" w:sz="4" w:color="BBBBBB"/></w:tblBorders></w:tblPr>`)
			for i, row := range b.Rows {
				body.WriteString(`<w:tr>`)
				for _, cell := range row {
					spans := cell
					if i == 0 {
						spans = make([]span, len(cell))
						for k, s := range cell {
							s.Bold = true
							spans[k] = s
						}
					}
					fmt.Fprintf(&body, `<w:tc><w:p>%s</w:p></w:tc>`, docxRuns(spans))
				}
				body.WriteString(`</w:tr>`)
			}
			body.WriteString(`</w:tbl><w:p/>`)
		}
	}
	doc := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
		body.String() +
		`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1134" w:right="1134" w:bottom="1134" w:left="1134" w:header="708" w:footer="708" w:gutter="0"/></w:sectPr>` +
		`</w:body></w:document>`
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
			`<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>` +
			`</Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
			`</Relationships>`},
		{"word/_rels/document.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>` +
			`</Relationships>`},
		{"word/styles.xml", docxStyles},
		{"word/document.xml", doc},
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, p := range parts {
		w, err := zw.Create(p.name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(p.body)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func docxRuns(spans []span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(`<w:r>`)
		if s.Bold || s.Italic || s.Code {
			b.WriteString(`<w:rPr>`)
			if s.Code {
				b.WriteString(`<w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:cs="Consolas"/>`)
			}
			if s.Bold {
				b.WriteString(`<w:b/>`)
			}
			if s.Italic {
				b.WriteString(`<w:i/>`)
			}
			b.WriteString(`</w:rPr>`)
		}
		fmt.Fprintf(&b, `<w:t xml:space="preserve">%s</w:t></w:r>`, escape(s.Text))
	}
	return b.String()
}

const docxStyles = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
	`<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
	`<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:cs="Calibri"/><w:sz w:val="22"/></w:rPr></w:rPrDefault>` +
	`<w:pPrDefault><w:pPr><w:spacing w:after="120" w:line="276" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults>` +
	`<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:before="360" w:after="120"/><w:outlineLvl w:val="0"/></w:pPr><w:rPr><w:b/><w:sz w:val="36"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:before="280" w:after="100"/><w:outlineLvl w:val="1"/></w:pPr><w:rPr><w:b/><w:sz w:val="30"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Heading3"><w:name w:val="heading 3"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:before="220" w:after="80"/><w:outlineLvl w:val="2"/></w:pPr><w:rPr><w:b/><w:sz w:val="26"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="ListParagraph"><w:name w:val="List Paragraph"/><w:basedOn w:val="Normal"/><w:pPr><w:spacing w:after="60"/></w:pPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Quote"><w:name w:val="Quote"/><w:basedOn w:val="Normal"/><w:pPr><w:ind w:left="567"/></w:pPr><w:rPr><w:i/><w:color w:val="555555"/></w:rPr></w:style>` +
	`<w:style w:type="paragraph" w:styleId="Code"><w:name w:val="Code"/><w:basedOn w:val="Normal"/><w:pPr><w:spacing w:after="0" w:line="240" w:lineRule="auto"/><w:shd w:val="clear" w:color="auto" w:fill="F2F2F2"/></w:pPr><w:rPr><w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:cs="Consolas"/><w:sz w:val="19"/></w:rPr></w:style>` +
	`<w:style w:type="table" w:styleId="Table"><w:name w:val="Table"/><w:tblPr><w:tblCellMar><w:left w:w="100" w:type="dxa"/><w:right w:w="100" w:type="dxa"/></w:tblCellMar></w:tblPr></w:style>` +
	`</w:styles>`
