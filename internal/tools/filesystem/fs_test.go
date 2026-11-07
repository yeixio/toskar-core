package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadWriteStayInsideWorkspace(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	write := NewWrite(root)
	read := NewRead(root)

	wrote, err := write.Execute(ctx, map[string]any{"path": "notes/a.txt", "content": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if wrote["bytes"] != 5 {
		t.Fatalf("bytes=%v", wrote["bytes"])
	}

	got, err := read.Execute(ctx, map[string]any{"path": "notes/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if got["content"] != "hello" {
		t.Fatalf("content=%v", got["content"])
	}

	if _, err := read.Execute(ctx, map[string]any{"path": "../outside.txt"}); err == nil || !strings.Contains(err.Error(), "escapes workspace") {
		t.Fatalf("escape error=%v", err)
	}
	outside := filepath.Join(filepath.Dir(root), "outside.txt")
	if _, err := write.Execute(ctx, map[string]any{"path": outside, "content": "no"}); err == nil {
		t.Fatal("absolute path outside the workspace was accepted")
	}
	if _, err := read.Execute(ctx, map[string]any{}); err == nil {
		t.Fatal("missing path was accepted")
	}
}

func TestReadTruncatesLongFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "long.txt"), []byte(strings.Repeat("a", 12010)), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := NewRead(root).Execute(context.Background(), map[string]any{"path": "long.txt"})
	if err != nil {
		t.Fatal(err)
	}
	content, _ := got["content"].(string)
	if !strings.HasSuffix(content, "\n…") || len(content) > 12010 {
		t.Fatalf("content length=%d", len(content))
	}
}

func TestSearchSkipsDependencyDirs(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "secret.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := NewSearch(root).Execute(context.Background(), map[string]any{"query": "txt"})
	if err != nil {
		t.Fatal(err)
	}
	matches, _ := got["matches"].([]string)
	for _, match := range matches {
		if strings.Contains(match, "node_modules") {
			t.Fatalf("matches=%v", matches)
		}
	}
	if len(matches) != 1 || !strings.HasSuffix(matches[0], "keep.txt") {
		t.Fatalf("matches=%v", matches)
	}
	if _, err := NewSearch(root).Execute(context.Background(), map[string]any{"query": "  "}); err == nil {
		t.Fatal("empty query was accepted")
	}
}

// A search finds words anywhere in a path, and patterns as models write
// them: *test* found nothing before (#959).
func TestNameMatches(t *testing.T) {
	for _, tc := range []struct {
		query, rel string
		want       bool
	}{
		{"tally", "pkg/tally.go", true},
		{"*test*", "tally_test.go", true},
		{"*test*", "pkg/tally_test.go", true},
		{"*.go", "pkg/tally.go", true},
		{"*.test.ts *_test.go", "tally_test.go", true},
		{"*test*", "tally.go", false},
		{"*.py", "tally.go", false},
	} {
		if got := nameMatches(tc.query, tc.rel); got != tc.want {
			t.Errorf("%q on %q: %v", tc.query, tc.rel, got)
		}
	}
}
