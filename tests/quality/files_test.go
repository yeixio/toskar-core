package quality

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/mimir"
)

// The files set (#510): every file type Toskar reads, attached and asked
// about, and every type it makes, asked for and then opened. The stub
// checks the plumbing: the file reached the model, and a made file holds
// what was asked. A real model is also held to its answers.

// Attachment is a file sent with a case's message. Make builds it from
// Content: "pdf", "docx", and "pptx" from Markdown (a pptx's slides split
// by lines of ---), "xlsx" from CSV, and anything else is Content as it is.
type Attachment struct {
	Name    string `json:"name"`
	Make    string `json:"make"`
	Content string `json:"content"`
}

// Bytes is the file.
func (a Attachment) Bytes() ([]byte, error) {
	switch a.Make {
	case "pdf":
		return artifacts.MarkdownToPDF(a.Content)
	case "docx":
		return artifacts.MarkdownToDOCX(a.Content)
	case "xlsx":
		return artifacts.CSVToXLSX("Sheet1", a.Content)
	case "pptx":
		return pptx(strings.Split(a.Content, "\n---\n"))
	case "png":
		return shapesPNG(a.Content)
	// Text, a chart, and a dialog, drawn for the vision cases (#449).
	case "text":
		return textPNG(a.Content)
	case "bars":
		return barsPNG(a.Content)
	case "dialog":
		return dialogPNG(a.Content)
	case "file":
		// A file kept in the repository, by its path from tests/quality.
		return os.ReadFile(filepath.FromSlash(a.Content))
	case "":
		return []byte(a.Content), nil
	}
	return nil, fmt.Errorf("%s: unknown make %q", a.Name, a.Make)
}

// pptx writes a minimal deck, one text box a slide, as PowerPoint lays
// slides out in the zip.
func pptx(slides []string) ([]byte, error) {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for i, s := range slides {
		w, err := z.Create(fmt.Sprintf("ppt/slides/slide%d.xml", i+1))
		if err != nil {
			return nil, err
		}
		var paras strings.Builder
		for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
			var esc bytes.Buffer
			_ = xml.EscapeText(&esc, []byte(line))
			paras.WriteString(`<a:p><a:r><a:t>` + esc.String() + `</a:t></a:r></a:p>`)
		}
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree><p:sp><p:txBody>` + paras.String() + `</p:txBody></p:sp></p:spTree></p:cSld></p:sld>`))
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// shapesPNG draws a picture from a description such as
// "size=512 bg=white shape=square color=blue count=3": count shapes of one
// color in a row on a plain background, for the cases that ask what a
// picture shows and for the image edits.
func shapesPNG(spec string) ([]byte, error) {
	opt := map[string]string{"size": "512", "bg": "white", "shape": "circle", "color": "red", "count": "1"}
	for _, f := range strings.Fields(spec) {
		if k, v, ok := strings.Cut(f, "="); ok {
			opt[k] = v
		}
	}
	colors := map[string]color.RGBA{
		"white": {255, 255, 255, 255}, "black": {0, 0, 0, 255}, "red": {220, 30, 30, 255},
		"blue": {30, 70, 220, 255}, "green": {30, 160, 60, 255}, "yellow": {240, 200, 20, 255},
	}
	bg, ok1 := colors[opt["bg"]]
	fg, ok2 := colors[opt["color"]]
	size, err1 := strconv.Atoi(opt["size"])
	count, err2 := strconv.Atoi(opt["count"])
	if !ok1 || !ok2 || err1 != nil || err2 != nil || size < 32 || count < 1 || count > 6 {
		return nil, fmt.Errorf("bad picture %q", spec)
	}
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(img, img.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)
	cell := size / count
	r := cell * 3 / 10
	for i := range count {
		cx, cy := cell*i+cell/2, size/2
		for y := cy - r; y <= cy+r; y++ {
			for x := cx - r; x <= cx+r; x++ {
				dx, dy := x-cx, y-cy
				if opt["shape"] == "square" || dx*dx+dy*dy <= r*r {
					img.Set(x, y, fg)
				}
			}
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// fileText is a file read the way Toskar reads an attachment.
func fileText(name string, data []byte) (string, error) {
	passages, err := mimir.FilePassages(name, data)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, p := range passages {
		b.WriteString(p.Title + "\n" + p.Body + "\n")
	}
	return b.String(), nil
}

// madeFileFailures checks the files the assistant made against wants,
// by extension: the file exists, opens, and holds each text.
func madeFileFailures(wants map[string][]string, made map[string][]byte) []string {
	var out []string
	exts := make([]string, 0, len(wants))
	for ext := range wants {
		exts = append(exts, ext)
	}
	sort.Strings(exts)
	for _, ext := range exts {
		name, data := "", []byte(nil)
		for n, d := range made {
			if strings.EqualFold(filepath.Ext(n), ext) {
				name, data = n, d
				break
			}
		}
		if name == "" {
			out = append(out, fmt.Sprintf("no %s file was made to open", ext))
			continue
		}
		text, err := fileText(name, data)
		if err != nil {
			out = append(out, fmt.Sprintf("%s doesn't open: %v", name, err))
			continue
		}
		for _, want := range wants[ext] {
			if !regexp.MustCompile("(?i)" + want).MatchString(text) {
				out = append(out, fmt.Sprintf("%s lacks %q; it holds %.300q", name, want, text))
			}
		}
	}
	return out
}

type filesFile struct {
	Cases []Case `json:"cases"`
}

func loadFileCases(t *testing.T) []Case {
	t.Helper()
	raw, err := os.ReadFile("files.json")
	if err != nil {
		t.Fatal(err)
	}
	var f filesFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return f.Cases
}

// readableTypes and writableTypes are what the set must cover: every type
// chat reads and files.create writes. A type added to Toskar without a
// case fails TestFileCasesCover.
var readableKinds = map[string]bool{".txt": true, ".md": true, ".csv": true, ".tsv": true, ".json": true, ".html": true, ".xlsx": true, ".pdf": true, ".docx": true, ".pptx": true, ".py": true}

var writableKinds = []string{".pdf", ".docx", ".xlsx", ".csv", ".md", ".html", ".json", ".py"}

// TestFileCasesCover checks the set itself: every attachment builds and
// reads, and every readable and writable kind has a case.
func TestFileCasesCover(t *testing.T) {
	read, made := map[string]bool{}, map[string]bool{}
	for _, c := range loadFileCases(t) {
		for _, a := range c.Attach {
			data, err := a.Bytes()
			if err != nil {
				t.Errorf("%s: %v", c.ID, err)
				continue
			}
			if _, err := fileText(a.Name, data); err != nil && (!c.RealOnly || !errors.Is(err, mimir.ErrScanned)) {
				t.Errorf("%s: %s doesn't read: %v", c.ID, a.Name, err)
			}
			read[strings.ToLower(filepath.Ext(a.Name))] = true
		}
		for ext := range c.Expect.FileText {
			made[ext] = true
		}
	}
	for ext := range readableKinds {
		if !read[ext] {
			t.Errorf("no case reads a %s file", ext)
		}
	}
	for _, ext := range writableKinds {
		if !made[ext] {
			t.Errorf("no case makes a %s file", ext)
		}
	}
}

// TestFilesQuality runs the files set. TOSKAR_QUALITY_FILES_REPORT writes
// a Markdown report.
func TestFilesQuality(t *testing.T) {
	d, build := qualityDriver(t)
	var report []reportRow
	for _, c := range loadFileCases(t) {
		if stopped(t, d, c) {
			break
		}
		if row, ok := runCase(t, d, c, nil); ok {
			report = append(report, row)
		}
	}
	passed := 0
	for _, row := range report {
		if len(row.Failures) == 0 {
			passed++
		}
	}
	rate := 1.0
	if len(report) > 0 {
		rate = float64(passed) / float64(len(report))
	}
	t.Logf("files: %d of %d cases passed (%.0f%%) with the %s model", passed, len(report), rate*100, d.Name())
	if path := config.Env("QUALITY_FILES_REPORT"); path != "" {
		md := strings.Replace(markdownReport(d.Name(), report, summarizeRows(report), minPass(d.Name())), "# Chat quality:", "# Files quality:", 1)
		writeReport(t, d, build, path, md)
	}
	if rate < minPass(d.Name()) {
		t.Errorf("%.0f%% of cases passed; at least %.0f%% must", rate*100, minPass(d.Name())*100)
	}
}
