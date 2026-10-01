package mimir

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRecognizer "reads" scanned pages and records what it was asked for.
type fakeRecognizer struct {
	mu    sync.Mutex
	calls [][]int
	fail  error
}

func (f *fakeRecognizer) RecognizePDF(_ context.Context, _ []byte, pages []int) (map[int]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]int(nil), pages...))
	if f.fail != nil {
		return nil, f.fail
	}
	out := map[int]string{}
	for _, p := range pages {
		out[p] = "Store Hours\nMonday to Friday: 8 am to 6 pm\nSunday: closed"
	}
	return out, nil
}

func (f *fakeRecognizer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func b64(raw []byte) string { return base64.StdEncoding.EncodeToString(raw) }

func testPDF(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestScannedPDFNeedsRecognition(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	src, err := s.Create(ctx, CreateInput{Kind: KindText, Filename: "hours.pdf", ContentBase64: b64(testPDF(t, "scanned.pdf"))})
	if err != nil {
		t.Fatal(err)
	}
	if src.Status != StatusFailed || !strings.Contains(src.Error, "text recognition") {
		t.Fatalf("without a recognizer: %+v", src)
	}

	rec := &fakeRecognizer{}
	s.SetRecognizer(rec)
	if err := s.Refresh(ctx, src.ID); err != nil {
		t.Fatal(err)
	}
	src, _ = s.Get(ctx, src.ID)
	if src.Status != StatusReady || src.ChunkCount != 1 {
		t.Fatalf("with a recognizer: %+v", src)
	}
	hits, _ := s.Search(ctx, SearchInput{Query: "Are you open on Sunday?", SourceIDs: []string{src.ID}})
	if len(hits) == 0 || hits[0].Title != "hours.pdf p.1" || !strings.Contains(hits[0].Body, "Sunday: closed") {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestOnlyScannedPagesAreRecognized(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	rec := &fakeRecognizer{}
	s.SetRecognizer(rec)
	src, err := s.Create(ctx, CreateInput{Kind: KindText, Filename: "policy.pdf", ContentBase64: b64(testPDF(t, "mixed.pdf"))})
	if err != nil {
		t.Fatal(err)
	}
	if src.Status != StatusReady || src.ChunkCount != 2 {
		t.Fatalf("source = %+v", src)
	}
	if rec.count() != 1 || len(rec.calls[0]) != 1 || rec.calls[0][0] != 2 {
		t.Fatalf("recognized %v; only page 2 is scanned", rec.calls)
	}
	hits, _ := s.Search(ctx, SearchInput{Query: "warranty policy", SourceIDs: []string{src.ID}})
	if len(hits) == 0 || hits[0].Title != "policy.pdf p.1" {
		t.Fatalf("the text page should still be read from its text layer: %+v", hits)
	}
}

func TestRecognitionIsRememberedAcrossRefreshes(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	rec := &fakeRecognizer{}
	s.SetRecognizer(rec)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hours.pdf"), testPDF(t, "scanned.pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("# Notes\n\nFirst."), 0o600); err != nil {
		t.Fatal(err)
	}
	src, err := s.Create(ctx, CreateInput{Kind: KindPath, Path: dir})
	if err != nil || src.Status != StatusReady {
		t.Fatalf("source = %+v, %v", src, err)
	}
	// Another file in the folder changes; the scanned PDF is not read again.
	later := time.Now().Add(2 * time.Second)
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("# Notes\n\nSecond."), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(filepath.Join(dir, "notes.md"), later, later)
	if err := s.Refresh(ctx, src.ID); err != nil {
		t.Fatal(err)
	}
	if rec.count() != 1 {
		t.Fatalf("recognized %d times", rec.count())
	}
}

func TestRecognitionFailureIsReported(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	s.SetRecognizer(&fakeRecognizer{fail: errors.New("text recognition could not be installed: no network")})
	src, err := s.Create(ctx, CreateInput{Kind: KindText, Filename: "policy.pdf", ContentBase64: b64(testPDF(t, "mixed.pdf"))})
	if err != nil {
		t.Fatal(err)
	}
	if src.Status != StatusFailed || !strings.Contains(src.Error, "1 scanned pages could not be read") || !strings.Contains(src.Error, "no network") {
		t.Fatalf("source = %+v", src)
	}
}

func TestScannedAttachmentPointsToKnowledge(t *testing.T) {
	_, err := FilePassages("hours.pdf", testPDF(t, "scanned.pdf"))
	if err == nil || !strings.Contains(err.Error(), "Knowledge page") {
		t.Fatalf("got %v", err)
	}
	if _, err := PDFText("hours.pdf", testPDF(t, "scanned.pdf")); !errors.Is(err, ErrNoText) {
		t.Fatalf("got %v", err)
	}
}
