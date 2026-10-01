package mimir

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// Limits keep one careless folder from filling the database.
const (
	maxFileBytes   = 50 << 20
	maxFolderFiles = 2000
	maxTableRows   = 200000
	chunkRunes     = 1200
)

// SupportedExtensions are the file types Mimir reads.
var SupportedExtensions = []string{".txt", ".md", ".markdown", ".csv", ".tsv", ".json", ".jsonl", ".html", ".htm", ".xlsx", ".pdf"}

// Editable reports whether a source file is text that can be edited in place.
func Editable(filename string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".xlsx", ".pdf":
		return false
	}
	return true
}

// FirstSheetAsCSV renders the first sheet with data in an .xlsx workbook as
// CSV text, so the training classifier reads spreadsheets like CSV exports.
func FirstSheetAsCSV(name string, raw []byte) (string, error) {
	docs, err := parseXLSX(name, raw)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	w := csv.NewWriter(&b)
	for _, d := range docs {
		if d.Header == nil {
			continue
		}
		_ = w.Write(d.Header)
		for _, r := range d.Rows {
			_ = w.Write(r)
		}
		break
	}
	w.Flush()
	return b.String(), w.Error()
}

func supported(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, e := range SupportedExtensions {
		if ext == e {
			return true
		}
	}
	return false
}

// document is one readable file.
type document struct {
	Name string
	// Text holds prose. Header and Rows hold a table.
	Text   string
	Header []string
	Rows   [][]string
}

// chunk is one searchable passage.
type chunk struct {
	Title string
	Body  string
}

// files lists the supported files under path, in a stable order.
func files(path string) ([]string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		if !supported(path) {
			return nil, fmt.Errorf("%s is not a supported file type (%s)", filepath.Base(path), strings.Join(SupportedExtensions, ", "))
		}
		return []string{path}, nil
	}
	var out []string
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != path && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || !supported(p) {
			return nil
		}
		out = append(out, p)
		if len(out) > maxFolderFiles {
			return fmt.Errorf("the folder has more than %d supported files; connect a smaller folder", maxFolderFiles)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("no supported files in %s (%s)", path, strings.Join(SupportedExtensions, ", "))
	}
	return out, nil
}

// signature changes when any file under path changes.
func signature(path string) (string, error) {
	list, err := files(path)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, f := range list {
		st, err := os.Stat(f)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s|%d|%d\n", f, st.Size(), st.ModTime().UnixNano())
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// readSource reads every supported file under path. readPDF reads PDFs;
// nil reads only their text layer.
func readSource(path string, readPDF pdfReader) ([]document, error) {
	if readPDF == nil {
		readPDF = parsePDF
	}
	list, err := files(path)
	if err != nil {
		return nil, err
	}
	root := path
	if st, err := os.Stat(path); err == nil && !st.IsDir() {
		root = filepath.Dir(path)
	}
	var docs []document
	for _, f := range list {
		st, err := os.Stat(f)
		if err != nil {
			return nil, err
		}
		if st.Size() > maxFileBytes {
			return nil, fmt.Errorf("%s is larger than %d MB", filepath.Base(f), maxFileBytes>>20)
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		name, _ := filepath.Rel(root, f)
		var multi func(string, []byte) ([]document, error)
		switch strings.ToLower(filepath.Ext(name)) {
		case ".xlsx":
			multi = parseXLSX
		case ".pdf":
			multi = readPDF
		}
		if multi != nil {
			parts, err := multi(name, raw)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			docs = append(docs, parts...)
			continue
		}
		doc, err := parseDocument(name, raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

func parseDocument(name string, raw []byte) (document, error) {
	if !utf8.Valid(raw) {
		return document{}, fmt.Errorf("the file is not UTF-8 text")
	}
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	switch strings.ToLower(filepath.Ext(name)) {
	case ".csv":
		return parseTable(name, raw, ',')
	case ".tsv":
		return parseTable(name, raw, '\t')
	case ".jsonl":
		return parseJSONLines(name, raw)
	case ".json":
		return parseJSON(name, raw)
	case ".html", ".htm":
		return document{Name: name, Text: stripHTML(string(raw))}, nil
	default:
		return document{Name: name, Text: string(raw)}, nil
	}
}

func parseTable(name string, raw []byte, sep rune) (document, error) {
	r := csv.NewReader(bytes.NewReader(raw))
	r.Comma = sep
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	records, err := r.ReadAll()
	if err != nil {
		return document{}, err
	}
	if len(records) == 0 {
		return document{Name: name}, nil
	}
	if len(records)-1 > maxTableRows {
		return document{}, fmt.Errorf("more than %d rows", maxTableRows)
	}
	return document{Name: name, Header: records[0], Rows: records[1:]}, nil
}

// parseJSONLines reads one object per line as a table row, keyed by the
// union of top-level fields.
func parseJSONLines(name string, raw []byte) (document, error) {
	var objs []map[string]any
	for i, line := range bytes.Split(raw, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal(line, &obj); err != nil {
			return document{}, fmt.Errorf("line %d is not a JSON object", i+1)
		}
		objs = append(objs, obj)
	}
	return objectsTable(name, objs), nil
}

func parseJSON(name string, raw []byte) (document, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return document{}, err
	}
	if list, ok := v.([]any); ok {
		objs := make([]map[string]any, 0, len(list))
		for _, item := range list {
			if obj, ok := item.(map[string]any); ok {
				objs = append(objs, obj)
			}
		}
		if len(objs) == len(list) && len(objs) > 0 {
			return objectsTable(name, objs), nil
		}
	}
	pretty, _ := json.MarshalIndent(v, "", "  ")
	return document{Name: name, Text: string(pretty)}, nil
}

func objectsTable(name string, objs []map[string]any) document {
	seen := map[string]bool{}
	var header []string
	for _, o := range objs {
		for k := range o {
			if !seen[k] {
				seen[k] = true
				header = append(header, k)
			}
		}
	}
	sort.Strings(header)
	rows := make([][]string, 0, len(objs))
	for _, o := range objs {
		row := make([]string, len(header))
		for i, k := range header {
			if v, ok := o[k]; ok && v != nil {
				if s, ok := v.(string); ok {
					row[i] = s
				} else {
					b, _ := json.Marshal(v)
					row[i] = string(b)
				}
			}
		}
		rows = append(rows, row)
	}
	return document{Name: name, Header: header, Rows: rows}
}

var (
	scriptRe = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
	tagRe    = regexp.MustCompile(`(?s)<[^>]+>`)
	blockRe  = regexp.MustCompile(`(?i)</?(p|div|br|li|tr|h[1-6]|section|article)[^>]*>`)
)

func stripHTML(s string) string {
	s = scriptRe.ReplaceAllString(s, " ")
	s = blockRe.ReplaceAllString(s, "\n\n")
	s = tagRe.ReplaceAllString(s, " ")
	return html.UnescapeString(s)
}

// chunkDocuments splits documents into passages. A table row is one passage,
// labelled with its column names, so a lookup by SKU or size finds that row.
func chunkDocuments(docs []document) []chunk {
	var out []chunk
	for _, d := range docs {
		if d.Header != nil {
			for i, row := range d.Rows {
				var b strings.Builder
				for j, cell := range row {
					cell = strings.TrimSpace(cell)
					if cell == "" {
						continue
					}
					col := fmt.Sprintf("column %d", j+1)
					if j < len(d.Header) && strings.TrimSpace(d.Header[j]) != "" {
						// "in_stock" reads as "in stock", which small models understand.
						col = strings.ReplaceAll(strings.TrimSpace(d.Header[j]), "_", " ")
					}
					if b.Len() > 0 {
						b.WriteString("; ")
					}
					b.WriteString(col + ": " + cell)
				}
				if b.Len() > 0 {
					out = append(out, chunk{Title: fmt.Sprintf("%s row %d", d.Name, i+1), Body: b.String()})
				}
			}
			continue
		}
		out = append(out, chunkText(d.Name, d.Text)...)
	}
	return out
}

// chunkText packs paragraphs into passages of about chunkRunes, carrying the
// nearest Markdown heading as the title.
func chunkText(name, text string) []chunk {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var out []chunk
	heading := ""
	var cur strings.Builder
	curHeading := ""
	flush := func() {
		body := strings.TrimSpace(cur.String())
		if body != "" {
			title := name
			if curHeading != "" {
				title = name + " — " + curHeading
			}
			out = append(out, chunk{Title: title, Body: body})
		}
		cur.Reset()
	}
	for _, para := range strings.Split(text, "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		if strings.HasPrefix(para, "#") {
			line := strings.SplitN(para, "\n", 2)[0]
			heading = strings.TrimSpace(strings.TrimLeft(line, "#"))
		}
		if cur.Len() > 0 && utf8.RuneCountInString(cur.String())+utf8.RuneCountInString(para) > chunkRunes {
			flush()
		}
		if cur.Len() == 0 {
			curHeading = heading
		}
		// A single paragraph longer than a chunk is split on runes.
		for utf8.RuneCountInString(para) > chunkRunes {
			r := []rune(para)
			cur.WriteString(string(r[:chunkRunes]))
			flush()
			curHeading = heading
			para = string(r[chunkRunes:])
		}
		if cur.Len() > 0 {
			cur.WriteString("\n\n")
		}
		cur.WriteString(para)
	}
	flush()
	return out
}
