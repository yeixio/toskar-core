package artifacts

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// CSVToXLSX turns CSV text into a one-sheet Excel workbook, so a spreadsheet
// the assistant writes opens in Excel, Numbers, and Sheets. Numbers stay
// numbers; everything else is text. The first row is bold as a header.
// sheetLine starts a new sheet in a workbook given as CSV: "## Sheet: Name".
var sheetLine = regexp.MustCompile(`(?im)^\s*#{1,3}\s*sheet\s*:\s*(.+?)\s*$`)

type sheetData struct {
	name string
	rows [][]string
}

// CSVToXLSX makes a workbook from CSV text. A line "## Sheet: Name" starts
// another sheet; a cell that starts with = is a formula, such as =SUM(B2:B9).
func CSVToXLSX(sheetName, text string) ([]byte, error) {
	var sheets []sheetData
	parse := func(name, body string) error {
		if strings.TrimSpace(body) == "" {
			return nil
		}
		r := csv.NewReader(strings.NewReader(body))
		r.FieldsPerRecord = -1
		r.LazyQuotes = true
		rows, err := r.ReadAll()
		if err != nil {
			return fmt.Errorf("the spreadsheet is not valid CSV: %w", err)
		}
		sheets = append(sheets, sheetData{name: name, rows: rows})
		return nil
	}
	marks := sheetLine.FindAllStringSubmatchIndex(text, -1)
	if len(marks) == 0 {
		if err := parse(sheetName, text); err != nil {
			return nil, err
		}
	} else {
		if err := parse(sheetName, text[:marks[0][0]]); err != nil {
			return nil, err
		}
		for i, m := range marks {
			end := len(text)
			if i+1 < len(marks) {
				end = marks[i+1][0]
			}
			if err := parse(text[m[2]:m[3]], text[m[1]:end]); err != nil {
				return nil, err
			}
		}
	}
	if len(sheets) == 0 {
		return nil, fmt.Errorf("the spreadsheet has no rows")
	}
	return writeWorkbook(sheets)
}

func column(i int) string {
	name := ""
	for i++; i > 0; i = (i - 1) / 26 {
		name = string(rune('A'+(i-1)%26)) + name
	}
	return name
}

func escape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// cleanSheetName makes a name Excel accepts, unique in the workbook.
func cleanSheetName(name string, used map[string]bool) string {
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`[]:*?/\`, r) {
			return ' '
		}
		return r
	}, strings.TrimSpace(name))
	if name == "" {
		name = "Sheet1"
	}
	if r := []rune(name); len(r) > 31 {
		name = string(r[:31])
	}
	base := name
	for n := 2; used[strings.ToLower(name)]; n++ {
		suffix := fmt.Sprintf(" %d", n)
		r := []rune(base)
		if len(r)+len(suffix) > 31 {
			r = r[:31-len(suffix)]
		}
		name = string(r) + suffix
	}
	used[strings.ToLower(name)] = true
	return name
}

func sheetXML(rows [][]string) string {
	var sheet strings.Builder
	sheet.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for i, row := range rows {
		fmt.Fprintf(&sheet, `<row r="%d">`, i+1)
		for j, cell := range row {
			ref := fmt.Sprintf("%s%d", column(j), i+1)
			style := ""
			if i == 0 {
				style = ` s="1"`
			}
			trimmed := strings.TrimSpace(cell)
			if strings.HasPrefix(trimmed, "=") && len(trimmed) > 1 {
				fmt.Fprintf(&sheet, `<c r="%s"%s><f>%s</f></c>`, ref, style, escape(trimmed[1:]))
				continue
			}
			if _, err := strconv.ParseFloat(trimmed, 64); err == nil && i > 0 && trimmed != "" {
				fmt.Fprintf(&sheet, `<c r="%s"%s><v>%s</v></c>`, ref, style, trimmed)
				continue
			}
			fmt.Fprintf(&sheet, `<c r="%s" t="inlineStr"%s><is><t xml:space="preserve">%s</t></is></c>`, ref, style, escape(cell))
		}
		sheet.WriteString(`</row>`)
	}
	sheet.WriteString(`</sheetData></worksheet>`)
	return sheet.String()
}

func writeWorkbook(sheets []sheetData) ([]byte, error) {
	used := map[string]bool{}
	var overrides, sheetEntries, rels strings.Builder
	files := []struct{ name, body string }{}
	for i, sh := range sheets {
		n := i + 1
		name := cleanSheetName(sh.name, used)
		fmt.Fprintf(&overrides, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, n)
		fmt.Fprintf(&sheetEntries, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, escape(name), n, n)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, n, n)
		files = append(files, struct{ name, body string }{fmt.Sprintf("xl/worksheets/sheet%d.xml", n), sheetXML(sh.rows)})
	}
	stylesID := len(sheets) + 1
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
			overrides.String() +
			`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>` +
			`</Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>` +
			`</Relationships>`},
		{"xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
			`<sheets>` + sheetEntries.String() + `</sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			rels.String() +
			fmt.Sprintf(`<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`, stylesID) +
			`</Relationships>`},
		{"xl/styles.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
			`<fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts>` +
			`<fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>` +
			`<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>` +
			`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
			`<cellXfs count="2"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/></cellXfs>` +
			`</styleSheet>`},
	}
	parts = append(parts, files...)
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
