package app

import (
	"context"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/models"
	"github.com/yeixio/toskar-core/internal/models/hfclient"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// storeListing is how many models each Hugging Face listing reads.
const storeListing = 24

// modelStore is the store's front page for this computer (#593). Without
// Hugging Face, it still has the models Toskar knows.
func (a *App) modelStore(ctx context.Context) (contracts.ModelStore, error) {
	if a.HF == nil {
		a.HF = hfclient.New()
	}
	var listings models.StoreListings
	var wg sync.WaitGroup
	var mu sync.Mutex
	live := false
	read := func(into *[]contracts.StoreModel, sort, search string) {
		defer wg.Done()
		list, err := a.HF.Listing(ctx, sort, search, storeListing)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			a.Logger.Warn("model store listing", "sort", sort, "error", err)
			return
		}
		*into, live = list, true
	}
	wg.Add(3)
	go read(&listings.Trending, "trendingScore", "")
	go read(&listings.Liked, "likes", "")
	go read(&listings.Code, "likes", "code")
	wg.Wait()

	hw, err := a.detectHardware(ctx)
	if err != nil {
		hw = contracts.HardwareInventory{}
	}
	installed := map[string]bool{}
	if list, err := a.Models.List(ctx); err == nil {
		for _, m := range list {
			if m.Installed {
				installed[m.ID] = true
			}
		}
	}
	cfg := a.Config.Get()
	running, _ := a.listRunningAll(ctx)
	shelves := models.StoreShelves(listings, models.TestedEntries(a.Models.Catalog().List()), installed, hw, a.fitOptions(ctx, cfg.NodeID, true, running), time.Now())
	return contracts.ModelStore{Shelves: shelves, Live: live}, nil
}

// qualityResults are the public quality results. Not being able to read them
// isn't an error: the page says so.
func (a *App) qualityResults(ctx context.Context) contracts.QualityResults {
	empty := contracts.QualityResults{Results: []contracts.QualityResult{}}
	if a.Ratings == nil {
		return empty
	}
	got, err := a.Ratings.QualityResults(ctx)
	if err != nil {
		a.Logger.Warn("quality results", "error", err)
		return empty
	}
	return got
}
