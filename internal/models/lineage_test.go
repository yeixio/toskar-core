package models

import (
	"strings"
	"testing"
)

// Every catalog model names its family and generation, and what it
// supersedes checks out (#593).
func TestCatalogLineage(t *testing.T) {
	c, err := NewCatalogEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	for _, err := range lineageErrors(c.List()) {
		t.Error(err)
	}
}

func TestLineageErrors(t *testing.T) {
	entry := func(id, family, generation string, supersedes ...string) CatalogEntry {
		return CatalogEntry{ID: id, Family: family, Generation: generation, Supersedes: supersedes}
	}
	cases := []struct {
		name string
		list []CatalogEntry
		want string
	}{
		{"fine", []CatalogEntry{entry("a2", "a", "2"), entry("a3", "a", "3", "a2")}, ""},
		{"no generation", []CatalogEntry{entry("a2", "a", "")}, "no family or generation"},
		{"missing", []CatalogEntry{entry("a3", "a", "3", "a2")}, "isn't in the catalog"},
		{"other family", []CatalogEntry{entry("b2", "b", "2"), entry("a3", "a", "3", "b2")}, "another family"},
		{"same generation", []CatalogEntry{entry("a3x", "a", "3"), entry("a3", "a", "3", "a3x")}, "same generation"},
		{"embedding for chat", []CatalogEntry{{ID: "e1", Family: "a", Generation: "2", SupportRole: "embedding"}, entry("a3", "a", "3", "e1")}, "another job"},
		{"circle", []CatalogEntry{entry("a2", "a", "2", "a3"), entry("a3", "a", "3", "a2")}, "supersedes itself"},
	}
	for _, tc := range cases {
		errs := lineageErrors(tc.list)
		if tc.want == "" {
			if len(errs) > 0 {
				t.Errorf("%s: %v", tc.name, errs)
			}
			continue
		}
		found := false
		for _, err := range errs {
			found = found || strings.Contains(err.Error(), tc.want)
		}
		if !found {
			t.Errorf("%s: want %q, got %v", tc.name, tc.want, errs)
		}
	}
}
