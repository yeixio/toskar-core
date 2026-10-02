package mimir

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

// Limits for a workbook, so a zip bomb or a huge sheet cannot exhaust memory.
const (
	maxXLSXPartBytes = 64 << 20
	maxXLSXSheets    = 50
)

// parseXLSX reads every worksheet of an Excel workbook as a table. The first
// non-empty row of a sheet is its header. Formula cells use the value Excel
// saved with the file.
// Sheet is one worksheet's cells as text, in rows.
type Sheet struct {
	Name string
	Rows [][]string
}

// ReadWorkbook reads an .xlsx workbook's sheets: every sheet up to the
// limit, with leading empty rows removed.
func ReadWorkbook(raw []byte) ([]Sheet, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("not an .xlsx workbook: %w", err)
	}
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	read := func(p string) ([]byte, error) {
		f, ok := files[p]
		if !ok {
			return nil, fmt.Errorf("%s is missing", p)
		}
		if f.UncompressedSize64 > maxXLSXPartBytes {
			return nil, fmt.Errorf("%s is larger than %d MB", p, maxXLSXPartBytes>>20)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(io.LimitReader(rc, maxXLSXPartBytes))
	}

	var shared []string
	if b, err := read("xl/sharedStrings.xml"); err == nil {
		if shared, err = parseSharedStrings(b); err != nil {
			return nil, err
		}
	}
	sheets, err := workbookSheets(read)
	if err != nil {
		return nil, err
	}
	if len(sheets) > maxXLSXSheets {
		sheets = sheets[:maxXLSXSheets]
	}
	var out []Sheet
	for _, sh := range sheets {
		b, err := read(sh.path)
		if err != nil {
			return nil, err
		}
		rows, err := parseSheet(b, shared)
		if err != nil {
			return nil, fmt.Errorf("sheet %s: %w", sh.name, err)
		}
		for len(rows) > 0 && rowEmpty(rows[0]) {
			rows = rows[1:]
		}
		out = append(out, Sheet{Name: sh.name, Rows: rows})
	}
	return out, nil
}

func parseXLSX(name string, raw []byte) ([]document, error) {
	sheets, err := ReadWorkbook(raw)
	if err != nil {
		return nil, err
	}
	var docs []document
	for _, sh := range sheets {
		rows := sh.Rows
		if len(rows) == 0 {
			continue
		}
		body := rows[1:]
		if len(body) > maxTableRows {
			return nil, fmt.Errorf("sheet %s has more than %d rows", sh.Name, maxTableRows)
		}
		docName := name
		if len(sheets) > 1 {
			docName = name + " › " + sh.Name
		}
		docs = append(docs, document{Name: docName, Header: rows[0], Rows: body})
	}
	if len(docs) == 0 {
		return []document{{Name: name}}, nil
	}
	return docs, nil
}

type sheetRef struct{ name, path string }

func workbookSheets(read func(string) ([]byte, error)) ([]sheetRef, error) {
	wb, err := read("xl/workbook.xml")
	if err != nil {
		return nil, err
	}
	var book struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			RID  string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := xml.Unmarshal(wb, &book); err != nil {
		return nil, err
	}
	targets := map[string]string{}
	if rels, err := read("xl/_rels/workbook.xml.rels"); err == nil {
		var r struct {
			Items []struct {
				ID     string `xml:"Id,attr"`
				Target string `xml:"Target,attr"`
			} `xml:"Relationship"`
		}
		if err := xml.Unmarshal(rels, &r); err != nil {
			return nil, err
		}
		for _, it := range r.Items {
			t := strings.TrimPrefix(it.Target, "/")
			if !strings.HasPrefix(t, "xl/") {
				t = path.Join("xl", t)
			}
			targets[it.ID] = t
		}
	}
	var out []sheetRef
	for i, s := range book.Sheets {
		p := targets[s.RID]
		if p == "" {
			p = fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1)
		}
		out = append(out, sheetRef{name: s.Name, path: p})
	}
	return out, nil
}

// richText is a string item: plain <t>, or rich text runs <r><t>.
type richText struct {
	T    string `xml:"t"`
	Runs []struct {
		T string `xml:"t"`
	} `xml:"r"`
}

func (r richText) String() string {
	if len(r.Runs) == 0 {
		return r.T
	}
	var b strings.Builder
	for _, run := range r.Runs {
		b.WriteString(run.T)
	}
	return b.String()
}

func parseSharedStrings(b []byte) ([]string, error) {
	var sst struct {
		Items []richText `xml:"si"`
	}
	if err := xml.Unmarshal(b, &sst); err != nil {
		return nil, err
	}
	out := make([]string, len(sst.Items))
	for i, it := range sst.Items {
		out[i] = it.String()
	}
	return out, nil
}

func parseSheet(b []byte, shared []string) ([][]string, error) {
	var ws struct {
		Rows []struct {
			Cells []struct {
				Ref    string   `xml:"r,attr"`
				Type   string   `xml:"t,attr"`
				Value  string   `xml:"v"`
				Inline richText `xml:"is"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	if err := xml.Unmarshal(b, &ws); err != nil {
		return nil, err
	}
	rows := make([][]string, 0, len(ws.Rows))
	for _, r := range ws.Rows {
		var row []string
		for i, c := range r.Cells {
			col := i
			if c.Ref != "" {
				col = columnIndex(c.Ref)
			}
			var v string
			switch c.Type {
			case "s":
				if n, err := strconv.Atoi(strings.TrimSpace(c.Value)); err == nil && n >= 0 && n < len(shared) {
					v = shared[n]
				}
			case "inlineStr":
				v = c.Inline.String()
			case "b":
				v = map[string]string{"1": "TRUE", "0": "FALSE"}[c.Value]
			default:
				v = c.Value
			}
			for len(row) < col {
				row = append(row, "")
			}
			if col < len(row) {
				row[col] = v
			} else {
				row = append(row, v)
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// columnIndex turns a cell reference such as "AB12" into a zero-based column.
func columnIndex(ref string) int {
	n := 0
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			break
		}
		n = n*26 + int(r-'A'+1)
	}
	return n - 1
}

func rowEmpty(row []string) bool {
	for _, c := range row {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}
