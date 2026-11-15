package mimir

import (
	"context"
	"strings"
	"testing"
)

const harborRefunds = "Harbor Supply refund policy\n\nCustomers may return unopened items within 45 days for a full refund. Opened items get store credit only.\n"

// A passage that shares one plain word with a longer question isn't
// brought in (#1217); one that shares more, or a code, still is.
func TestOneSharedWordIsntAMatch(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if _, err := s.Create(ctx, CreateInput{Kind: KindText, Filename: "refunds.md", Text: harborRefunds}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		query string
		found bool
	}{
		{"Any good coffee shops near the harbor in Juneau?", false},
		{"How long do customers have to return unopened items?", true},
		{"refund policy", true},
		{"Harbor Supply", true},
		{"Can I return item 45 after opening it at the harbor?", true},
	} {
		hits, err := s.Search(ctx, SearchInput{Query: c.query})
		if err != nil {
			t.Fatal(err)
		}
		if got := len(hits) > 0; got != c.found {
			t.Errorf("%q: found %v, want %v", c.query, got, c.found)
		}
	}
}

func TestContextBlockLetsAnUnrelatedPassageGo(t *testing.T) {
	b := ContextBlock([]Hit{{Title: "refunds.md", Body: harborRefunds}}, 0)
	if !strings.Contains(b, "If they are about something else, leave them out and answer as you would without them.") {
		t.Fatalf("block: %s", b)
	}
}
