package models

import (
	"path"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

func storeItem(repo string, size uint64, likes int, age time.Duration, tags ...string) contracts.StoreModel {
	return contracts.StoreModel{
		// As Hugging Face names read: no owner, no -GGUF.
		BrowseModel: contracts.BrowseModel{ID: repo, DisplayName: strings.TrimSuffix(path.Base(repo), "-GGUF"), RepoID: repo, SizeBytes: size, Variant: "Q4_K_M", Tags: tags},
		Likes:       likes, CreatedAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC).Add(-age),
	}
}

func shelfOf(shelves []contracts.StoreShelf, id string) []string {
	for _, s := range shelves {
		if s.ID == id {
			out := []string{}
			for _, m := range s.Models {
				out = append(out, m.ID)
			}
			return out
		}
	}
	return nil
}

func TestStoreShelves(t *testing.T) {
	hw := contracts.HardwareInventory{Memory: contracts.MemoryInfo{TotalBytes: 16_000_000_000, AvailableBytes: 12_000_000_000}}
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	const day = 24 * time.Hour
	listings := StoreListings{
		Trending: []contracts.StoreModel{
			storeItem("org/Fresh", 2_000_000_000, 100, 5*day, "general"),
			storeItem("org/Unnoticed", 2_000_000_000, 1, 1*day),
			storeItem("org/Huge", 90_000_000_000, 900, 5*day),
			storeItem("org/Old", 5_000_000_000, 500, 400*day, "general"),
			storeItem("other/Old-GGUF", 5_000_000_000, 40, 400*day),
			storeItem("org/Split", 0, 900, 5*day),
		},
		Liked: []contracts.StoreModel{storeItem("org/Sees", 4_000_000_000, 700, 300*day, "vision")},
		Code:  []contracts.StoreModel{storeItem("org/Coder-7B", 4_000_000_000, 300, 300*day, "coding"), storeItem("org/Decoded-Stories", 4_000_000_000, 300, 300*day)},
	}
	catalog := []CatalogEntry{
		{ID: "task", DisplayName: "Task", SizeBytes: 5_000_000_000, MemoryNeededBytes: 6_000_000_000, Tags: []string{"coding", "tasks"}, Runtime: []string{"llamacpp"}, Source: contracts.ModelSource{URL: "https://huggingface.co/cat/Task-GGUF/resolve/main/t.gguf"}},
		{ID: "coder", DisplayName: "Coder", SizeBytes: 6_000_000_000, MemoryNeededBytes: 7_000_000_000, Tags: []string{"coding"}, Runtime: []string{"llamacpp"}, Source: contracts.ModelSource{URL: "https://huggingface.co/cat/Coder-GGUF/resolve/main/c.gguf"}},
		{ID: "embed", DisplayName: "Embed", SizeBytes: 1_000_000, SupportRole: "embedding", Runtime: []string{"llamacpp"}},
		{ID: "giant", DisplayName: "Giant", SizeBytes: 90_000_000_000, MemoryNeededBytes: 100_000_000_000, Runtime: []string{"llamacpp"}},
	}
	shelves := StoreShelves(listings, catalog, map[string]bool{"org/Sees": true}, "task", hw, FitOptions{}, now)

	equal := func(name string, got []string, want ...string) {
		t.Helper()
		if len(got) != len(want) {
			t.Errorf("%s: got %v, want %v", name, got, want)
			return
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s: got %v, want %v", name, got, want)
				return
			}
		}
	}
	// Doesn't fit: gone. Another upload of one model: one kept.
	// A tested model first, then what's popular and fits well.
	equal("picks", shelfOf(shelves, "picks"), "task", "org/Fresh", "org/Unnoticed")
	equal("popular", shelfOf(shelves, "popular"), "org/Fresh", "org/Unnoticed", "org/Old")
	equal("new", shelfOf(shelves, "new"), "org/Fresh")
	equal("top_rated", shelfOf(shelves, "top_rated"), "org/Sees")
	// Measured to finish tasks first, then what's named for code.
	equal("code", shelfOf(shelves, "code"), "task", "org/Coder-7B", "coder")
	equal("everyday", shelfOf(shelves, "everyday"), "org/Old", "org/Fresh")
	equal("sees", shelfOf(shelves, "sees"), "org/Sees")
	equal("small", shelfOf(shelves, "small"), "org/Fresh", "org/Unnoticed")
	equal("tested", shelfOf(shelves, "tested"), "coder", "task")
	for _, s := range shelves {
		for _, m := range s.Models {
			if m.ID == "org/Sees" && !m.Installed {
				t.Errorf("%s: not installed: %+v", s.ID, m)
			}
			if m.Fit == nil {
				t.Errorf("%s: no fit: %+v", s.ID, m)
			}
			if m.ID == "task" && (m.CatalogID != "task" || m.DetailsURL != "https://huggingface.co/cat/Task-GGUF") {
				t.Errorf("%+v", m)
			}
		}
	}
	// Without Hugging Face, Toskar's own models are still there.
	offline := StoreShelves(StoreListings{}, catalog, nil, "task", hw, FitOptions{}, now)
	if len(offline) != 3 || offline[0].ID != "picks" || offline[1].ID != "code" || offline[2].ID != "tested" {
		t.Errorf("offline: %+v", offline)
	}
	// Qwen2.5 and Qwen 2.5 are one model, on one shelf.
	spaced := storeItem("a/Qwen2.5-Coder-7B", 4_000_000_000, 5, 5*day)
	spaced.DisplayName = "Qwen2.5 Coder 7B"
	other := storeItem("b/Qwen-2.5-Coder-7B", 4_000_000_000, 5, 5*day)
	other.DisplayName = "Qwen 2.5 Coder 7B"
	got := StoreShelves(StoreListings{Trending: []contracts.StoreModel{spaced, other}}, nil, nil, "", hw, FitOptions{}, now)
	if len(got[0].Models) != 1 {
		t.Errorf("%+v", got[0].Models)
	}
	// A listed model from a catalog repository is that entry.
	same := StoreListings{Trending: []contracts.StoreModel{storeItem("cat/Task-GGUF", 5_000_000_000, 10, 5*day)}}
	got = StoreShelves(same, catalog, map[string]bool{"task": true}, "", hw, FitOptions{}, now)
	if m := got[0].Models[0]; m.CatalogID != "task" || !m.Installed {
		t.Errorf("%+v", m)
	}
}

// A model someone added is in the catalog, and isn't tested for being there.
func TestTestedEntriesLeaveOutAddedModels(t *testing.T) {
	shipped, err := NewCatalogEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	real := shipped.List()[0]
	got := TestedEntries([]CatalogEntry{real, {ID: "liquidai-lfm3-1-7b-gguf-q4-k-m"}})
	if len(got) != 1 || got[0].ID != real.ID {
		t.Fatalf("%+v", got)
	}
}
