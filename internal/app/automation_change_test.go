package app

import (
	"testing"

	"github.com/yeixio/toskar-core/internal/automations"
)

// A run's sources are what its read-only tools returned: the same reads
// give the same fingerprint, a different page a different one, and a tool
// that changes things isn't counted (#204).
func TestSourceRecorder(t *testing.T) {
	read := func(page string) string {
		var s sourceRecorder
		s.add("internet.search", map[string]any{"query": "jobs"}, map[string]any{"results": []any{"a", "b"}})
		s.add("internet.open", map[string]any{"url": "https://example.com/jobs"}, map[string]any{"content": page})
		return s.sum()
	}
	first, again := read("3 openings"), read("3 openings")
	if first != again {
		t.Fatal("the same reads gave different fingerprints")
	}
	if read("3 openings") == read("4 openings") {
		t.Fatal("a changed page kept its fingerprint")
	}
	var none sourceRecorder
	if none.sum() != "" {
		t.Fatal("a run that read nothing has a fingerprint")
	}
	var writes sourceRecorder
	writes.add("files.create", map[string]any{"name": "x.md"}, map[string]any{"id": "1"})
	if writes.sum() != "" {
		t.Fatal("a tool that creates files was counted as a source")
	}
}

// The run's page is the first web page a read-only tool opened: the page an
// automation watches, for a tap on the phone to open (#460).
func TestSourceRecorderKeepsTheFirstPage(t *testing.T) {
	var s sourceRecorder
	s.add("internet.search", map[string]any{"query": "trail runner size 10"}, map[string]any{"results": []any{"a"}})
	s.add("internet.open", map[string]any{"url": "file:///etc/passwd"}, map[string]any{"content": "x"})
	s.add("internet.open", map[string]any{"url": "https://shop.example/trail-runner"}, map[string]any{"content": "In stock"})
	s.add("browser.open", map[string]any{"url": "https://shop.example/other"}, map[string]any{"content": "y"})
	if got := s.firstPage(); got != "https://shop.example/trail-runner" {
		t.Fatalf("page %q", got)
	}
	var none sourceRecorder
	none.add("internet.search", map[string]any{"query": "q"}, map[string]any{})
	if none.firstPage() != "" {
		t.Fatal("a run that opened no page has one")
	}
}

// A watched page is the notification's page even when the run, told what
// changed, didn't open it again (#460).
func TestRunPageIsTheWatchedPage(t *testing.T) {
	watched := &automations.Trigger{Kind: automations.TriggerPage, URL: "https://shop.example/trail-runner"}
	if got := runPage(watched, ""); got != watched.URL {
		t.Fatalf("watched page: %q", got)
	}
	if got := runPage(watched, "https://shop.example/other"); got != watched.URL {
		t.Fatalf("watched page loses to an opened one: %q", got)
	}
	feed := &automations.Trigger{Kind: automations.TriggerFeed, URL: "https://news.example/feed.xml"}
	if got := runPage(feed, ""); got != "" {
		t.Fatalf("a feed is not a page to open: %q", got)
	}
	if got := runPage(nil, "https://shop.example/a"); got != "https://shop.example/a" {
		t.Fatalf("opened page: %q", got)
	}
}
