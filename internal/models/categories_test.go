package models

import (
	"testing"

	"github.com/yeixio/toskar-core/internal/taxonomy"
)

// Every catalog model says what it does (#593): one without categories, or
// with an ID categories.json doesn't have, fails here.
func TestCatalogCategories(t *testing.T) {
	c, err := NewCatalogEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	for _, err := range categoryErrors(c.List()) {
		t.Error(err)
	}
	for _, e := range c.List() {
		m := entryToContract(e, false)
		if e.Capabilities.Vision && !hasAll(m.InputModalities, "image") {
			t.Errorf("%s sees pictures but doesn't take image input", e.ID)
		}
		if m.SupportRole == "embedding" && m.PrimaryCategory != "retrieval" {
			t.Errorf("%s is an embedding model in %s", e.ID, m.PrimaryCategory)
		}
	}
}

func TestCategoryErrors(t *testing.T) {
	chat := taxonomy.Profile{PrimaryCategory: "chat_coding", Categories: []string{"chat_coding"}, Tasks: []string{"conversation"}, InputModalities: []string{"text"}, OutputModalities: []string{"text"}}
	if errs := categoryErrors([]CatalogEntry{{ID: "m", Profile: chat}}); len(errs) != 0 {
		t.Fatalf("a fine entry: %v", errs)
	}
	if errs := categoryErrors([]CatalogEntry{{ID: "m"}}); len(errs) != 1 {
		t.Fatalf("an entry without categories: %v", errs)
	}
	// A model added from a link is uncategorized, and that's fine.
	added := CatalogEntry{ID: "added", Dynamic: true}
	if errs := categoryErrors([]CatalogEntry{added}); len(errs) != 0 {
		t.Fatalf("a dynamic entry: %v", errs)
	}
	if m := entryToContract(added, true); m.PrimaryCategory != taxonomy.Uncategorized || len(m.Categories) != 1 {
		t.Fatalf("a dynamic entry shows as %q %v", m.PrimaryCategory, m.Categories)
	}
}

func hasAll(list []string, want ...string) bool {
	for _, w := range want {
		found := false
		for _, v := range list {
			found = found || v == w
		}
		if !found {
			return false
		}
	}
	return true
}
