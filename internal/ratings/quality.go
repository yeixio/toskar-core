package ratings

import (
	"context"
	"net/http"
	"time"

	"github.com/yeixio/toskar-core/internal/cache"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// DefaultQualityURL is the public quality results.
const DefaultQualityURL = "https://raw.githubusercontent.com/yeixio/toskar-model-data/main/quality/results.json"

// QualityCachePolicy keeps the results for an hour: they change weekly.
var QualityCachePolicy = cache.Policy{
	Name: "quality_results", Label: "Quality results", Key: "one list",
	TTL: time.Hour, Invalidation: "age", Scope: "this computer", Privacy: cache.Public, MaxEntries: 1,
}

var qualityCache = cache.New[contracts.QualityResults](QualityCachePolicy)

// QualityResults reads the public quality results.
func (c *Client) QualityResults(ctx context.Context) (contracts.QualityResults, error) {
	if kept, ok := qualityCache.Get("results"); ok {
		return kept, nil
	}
	url := c.QualityURL
	if url == "" {
		url = DefaultQualityURL
	}
	var out contracts.QualityResults
	if err := c.do(ctx, http.MethodGet, url, nil, nil, &out); err != nil {
		return contracts.QualityResults{}, err
	}
	out.Live = true
	qualityCache.Put("results", out)
	return out, nil
}

// QualityResults reads the public quality results.
func (s *Service) QualityResults(ctx context.Context) (contracts.QualityResults, error) {
	return s.Client.QualityResults(ctx)
}
