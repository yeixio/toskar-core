package models

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/gguf/gguftest"
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

// A model another supersedes points to the newest in its line (#593).
func TestNewer(t *testing.T) {
	entry := func(id string, supersedes ...string) CatalogEntry {
		return CatalogEntry{ID: id, Family: "a", Supersedes: supersedes}
	}
	got := Newer([]CatalogEntry{entry("a2"), entry("a3", "a2"), entry("a4", "a3"), entry("b1"), entry("x", "y"), entry("y", "x")})
	if got["a2"] != "a4" || got["a3"] != "a4" || got["a4"] != "" || got["b1"] != "" {
		t.Fatalf("%v", got)
	}
	// A circle, which the catalog's checks refuse, ends without hanging
	// and points nowhere newer.
	if got["x"] != "y" && got["x"] != "" {
		t.Fatalf("circle: %v", got)
	}
}

// An installed model says which newer one replaces it, until that one is
// installed too (#593).
func TestListSaysNewer(t *testing.T) {
	m, storage, dir := importManager(t)
	ctx := context.Background()
	for _, e := range []CatalogEntry{
		{ID: "old", DisplayName: "Old 3B", Family: "a", Generation: "1"},
		{ID: "new", DisplayName: "New 4B", Family: "a", Generation: "2", Supersedes: []string{"old"}},
		{ID: "spare", DisplayName: "Spare", Family: "b", Generation: "1"},
	} {
		m.catalog.Upsert(e)
		if err := storage.UpsertCatalogEntry(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	path := gguftest.Write(t, filepath.Join(dir, "old"), "old.gguf", gguftest.Options{Name: "Old"})
	if err := storage.MarkInstalled(ctx, "old", path, "", 1); err != nil {
		t.Fatal(err)
	}
	newer := func() map[string]string {
		list, err := m.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, model := range list {
			if model.Newer != "" {
				out[model.ID] = model.Newer
			}
		}
		return out
	}
	if got := newer(); len(got) != 1 || got["old"] != "new" {
		t.Fatalf("newer: %v", got)
	}
	if err := storage.MarkInstalled(ctx, "new", path, "", 1); err != nil {
		t.Fatal(err)
	}
	if got := newer(); len(got) != 0 {
		t.Fatalf("with the newer one installed: %v", got)
	}
}
