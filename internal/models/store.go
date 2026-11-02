package models

import (
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

const (
	// shelfSize is how many models a shelf shows.
	shelfSize = 8
	// smallSize is the largest model on the small and fast shelf.
	smallSize = 3_000_000_000
	// newFor is how long a model counts as new.
	newFor = 60 * 24 * time.Hour
	// newLikes keeps uploads nobody has noticed off the new shelf: Hugging
	// Face's newest is mostly those.
	newLikes = 20
)

// memoryForSize estimates what a model needs from its file: the catalog's
// entries need about this much more than their file, for context.
const memoryForSize = 1.2

var (
	codeNames = regexp.MustCompile(`(?i)cod(e|er|ing|estral)\b|devstral|starcoder|programming`)
	// quantWords differ between uploads of one model.
	// notNameChars differ too: Qwen2.5 and Qwen 2.5.
	notNameChars = regexp.MustCompile(`[^\pL\pN.]`)
	sameModel    = regexp.MustCompile(`(?i)[-_ ](gguf|q\d\w*|imatrix|i1|bf16|f16|instruct|chat)\b`)
)

// StoreListings are the store's raw listings from Hugging Face.
type StoreListings struct {
	Trending, Liked, Code []contracts.StoreModel
}

// StoreShelves arranges the store's front page: Hugging Face's rankings for
// this computer, and Toskar's tested models. What doesn't fit here is left out,
// so anything on a shelf can be installed and run. now is for the new shelf.
func StoreShelves(listings StoreListings, catalog []CatalogEntry, installed map[string]bool, hw contracts.HardwareInventory, opts FitOptions, now time.Time) []contracts.StoreShelf {
	source := map[string]string{}
	for _, e := range catalog {
		source[entryRepo(e)] = e.ID
	}
	prepare := func(list []contracts.StoreModel) []contracts.StoreModel {
		out := make([]contracts.StoreModel, 0, len(list))
		for _, m := range list {
			if id, ok := source[m.RepoID]; ok {
				m.CatalogID = id
			}
			m.Installed = installed[m.ID] || installed[m.CatalogID]
			// No size means no saying it fits, and usually a model in parts
			// that install from a link can't fetch.
			fit := storeFit(m, hw, opts)
			if fit == nil || !runsHere(*fit) {
				continue
			}
			m.Fit = fit
			out = append(out, m)
		}
		return out
	}
	trending, liked, code := prepare(listings.Trending), prepare(listings.Liked), prepare(listings.Code)
	everything := append(append(slices.Clone(trending), liked...), code...)

	tested := testedModels(catalog, installed, hw, opts)
	// Measured to finish tasks first, then Hugging Face's ranking, then the
	// rest of what's been tested for code.
	var finishes, testedCode []contracts.StoreModel
	for _, m := range tested {
		switch {
		case slices.Contains(m.Tags, "tasks"):
			finishes = append(finishes, m)
		case slices.Contains(m.Tags, "coding"):
			testedCode = append(testedCode, m)
		}
	}
	rankedCode := filterStore(code, func(m contracts.StoreModel) bool {
		return codeNames.MatchString(m.RepoID) || slices.Contains(m.Tags, "coding")
	})

	fresh := filterStore(everything, func(m contracts.StoreModel) bool {
		return m.Likes >= newLikes && !m.CreatedAt.IsZero() && now.Sub(m.CreatedAt) < newFor
	})
	slices.SortStableFunc(fresh, func(a, b contracts.StoreModel) int { return b.CreatedAt.Compare(a.CreatedAt) })
	sees := filterStore(everything, func(m contracts.StoreModel) bool { return slices.Contains(m.Tags, "vision") })
	slices.SortStableFunc(sees, byLikes)
	small := filterStore(everything, func(m contracts.StoreModel) bool { return m.SizeBytes > 0 && m.SizeBytes <= smallSize })
	slices.SortStableFunc(small, byLikes)

	shelves := []contracts.StoreShelf{
		{ID: "popular", Models: trending},
		{ID: "top_rated", Models: liked},
		{ID: "new", Models: fresh},
		{ID: "code", Models: slices.Concat(finishes, rankedCode, testedCode)},
		{ID: "sees", Models: sees},
		{ID: "small", Models: small},
		{ID: "tested", Models: tested},
	}
	out := shelves[:0]
	for _, s := range shelves {
		s.Models = firstOfEach(s.Models)
		if len(s.Models) > 0 {
			out = append(out, s)
		}
	}
	return out
}

func byLikes(a, b contracts.StoreModel) int { return b.Likes - a.Likes }

func filterStore(list []contracts.StoreModel, keep func(contracts.StoreModel) bool) []contracts.StoreModel {
	var out []contracts.StoreModel
	for _, m := range list {
		if keep(m) {
			out = append(out, m)
		}
	}
	return out
}

// firstOfEach keeps one upload of each model, up to a shelf's worth. Many people
// upload the same model, and a shelf of them shows nothing.
func firstOfEach(list []contracts.StoreModel) []contracts.StoreModel {
	seen := map[string]bool{}
	var out []contracts.StoreModel
	for _, m := range list {
		key := notNameChars.ReplaceAllString(strings.ToLower(sameModel.ReplaceAllString(m.DisplayName, "")), "")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, m)
		if len(out) == shelfSize {
			break
		}
	}
	return out
}

// runsHere is a fit that lets a model run on this computer, if slowly.
func runsHere(fit contracts.ModelFit) bool {
	return fit.InstallAllowed && fit.Label != contracts.FitUnsupported && fit.Label != contracts.FitTooLarge
}

// storeFit estimates how a Hugging Face model fits, from its size. Without a
// size there's no saying.
func storeFit(m contracts.StoreModel, hw contracts.HardwareInventory, opts FitOptions) *contracts.ModelFit {
	if m.SizeBytes == 0 {
		return nil
	}
	fit := ClassifyFit(CatalogEntry{
		ID: m.ID, DisplayName: m.DisplayName, Variant: m.Variant, SizeBytes: m.SizeBytes,
		MemoryNeededBytes: uint64(float64(m.SizeBytes) * memoryForSize), Runtime: []string{"llamacpp"},
	}, hw, "", "", opts)
	return &fit
}

// testedModels are the catalog's chat models that run here, best first.
func testedModels(catalog []CatalogEntry, installed map[string]bool, hw contracts.HardwareInventory, opts FitOptions) []contracts.StoreModel {
	var out []contracts.StoreModel
	// Newer and larger first: the catalog is ordered by neither.
	entries := slices.Clone(catalog)
	slices.SortStableFunc(entries, func(a, b CatalogEntry) int {
		if a.SizeBytes != b.SizeBytes {
			if a.SizeBytes > b.SizeBytes {
				return -1
			}
			return 1
		}
		return 0
	})
	for _, e := range entries {
		if e.Supporting() {
			continue
		}
		fit := ClassifyFit(e, hw, "", "", opts)
		if !runsHere(fit) {
			continue
		}
		repo := entryRepo(e)
		out = append(out, contracts.StoreModel{
			BrowseModel: contracts.BrowseModel{
				ID: e.ID, DisplayName: e.DisplayName, Summary: e.Summary, RepoID: repo, SourceURL: e.Source.URL,
				SizeBytes: e.SizeBytes, Parameters: e.Parameters, Variant: e.Variant, Tags: e.Tags,
			},
			Fit: &fit, CatalogID: e.ID, Installed: installed[e.ID], DetailsURL: "https://huggingface.co/" + repo,
		})
	}
	return out
}

// entryRepo is the Hugging Face repository a catalog entry comes from.
func entryRepo(e CatalogEntry) string {
	path, ok := strings.CutPrefix(e.Source.URL, "https://huggingface.co/")
	if !ok {
		return ""
	}
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "/" + parts[1]
}
