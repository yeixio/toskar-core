package hfclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/cache"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// StoreCachePolicy is the store listing's cache policy. Rankings change slowly,
// and a person browsing shouldn't wait on Hugging Face at every look.
var StoreCachePolicy = cache.Policy{
	Name: "model_store", Label: "Model store listing", Key: "the listing's sort and search",
	TTL: time.Hour, Invalidation: "age", Scope: "this computer", Privacy: cache.Public, MaxEntries: 16,
}

// notForTheStore are names the store's shelves leave out: re-uploads with their
// safeguards removed. Search can still find them.
var notForTheStore = regexp.MustCompile(`(?i)uncensored|abliterated|heretic|nsfw|erotic|roleplay`)

// sizeLookups is how many repositories' file sizes are read at once.
const sizeLookups = 6

type storeRepo struct {
	hfModel
	Likes     int       `json:"likes"`
	CreatedAt time.Time `json:"createdAt"`
	// Gated is false, or "auto" or "manual" for a model that needs asking.
	Gated any `json:"gated"`
}

// Listing is GGUF models Toskar can run, by a Hugging Face sort: trendingScore,
// likes, or createdAt. search narrows it.
func (c *Client) Listing(ctx context.Context, sort, search string, limit int) ([]contracts.StoreModel, error) {
	key := sort + "|" + strings.ToLower(search) + "|" + fmt.Sprint(limit)
	if c.StoreCache == nil {
		c.StoreCache = cache.New[[]contracts.StoreModel](StoreCachePolicy)
	}
	if cached, ok := c.StoreCache.Get(key); ok {
		return cached, nil
	}
	u, _ := url.Parse(c.BaseURL + "/models")
	vals := u.Query()
	vals.Set("filter", "gguf")
	vals.Set("pipeline_tag", "text-generation")
	vals.Set("sort", sort)
	vals.Set("direction", "-1")
	vals.Set("full", "true")
	// Many are left out, so ask for more than a shelf holds.
	vals.Set("limit", fmt.Sprint(limit*4))
	if search != "" {
		vals.Set("search", search)
	}
	u.RawQuery = vals.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Toskar/0.1")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("huggingface api status %d", resp.StatusCode)
	}
	var raw []storeRepo
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	var out []contracts.StoreModel
	for _, r := range raw {
		if m, ok := storeModel(r); ok {
			out = append(out, m)
		}
		if len(out) == limit {
			break
		}
	}
	c.withSizes(ctx, out)
	c.StoreCache.Put(key, out)
	return out, nil
}

// storeModel is a repository as the store shows it, if the store shows it: not
// gated, not an unsafe re-upload, and with a file in a standard size that this
// build can run, since a shelf shouldn't offer what may not load.
func storeModel(r storeRepo) (contracts.StoreModel, bool) {
	if gated, isBool := r.Gated.(bool); r.Gated != nil && (!isBool || gated) {
		return contracts.StoreModel{}, false
	}
	b := mapRepo(r.hfModel)
	if b.SourceURL == "" || notForTheStore.MatchString(b.RepoID) || notForTheStore.MatchString(strings.Join(r.Tags, " ")) {
		return contracts.StoreModel{}, false
	}
	if !slices.Contains(preferredQuants, strings.ToLower(b.Variant)) || strings.Contains(strings.ToLower(b.Filename), "gsq") {
		return contracts.StoreModel{}, false
	}
	license := ""
	for _, tag := range r.Tags {
		if name, ok := strings.CutPrefix(tag, "license:"); ok {
			license = name
		}
	}
	b.Summary = ""
	return contracts.StoreModel{
		BrowseModel: b, Likes: r.Likes, CreatedAt: r.CreatedAt, License: license,
		DetailsURL: "https://huggingface.co/" + b.RepoID,
	}, true
}

// withSizes reads each model's file size, which the listing leaves out and fit
// depends on. A model whose size can't be read keeps none, and has no fit.
func (c *Client) withSizes(ctx context.Context, models []contracts.StoreModel) {
	var wg sync.WaitGroup
	slots := make(chan struct{}, sizeLookups)
	for i := range models {
		if models[i].SizeBytes > 0 {
			continue
		}
		wg.Add(1)
		go func(m *contracts.StoreModel) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			files, err := c.tree(ctx, m.RepoID, "main")
			if err != nil {
				return
			}
			for _, f := range files {
				if f.Path == m.Filename {
					m.SizeBytes = f.Size
				}
			}
		}(&models[i])
	}
	wg.Wait()
}
