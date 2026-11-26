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

// SettingQuality reads the public quality results when Models › Results is
// opened; off, nothing is asked for (#1415).
const SettingQuality = "quality_results"

// QualityResults reads the public quality results, unless the person
// turned them off. A read that isn't kept from the last hour is recorded
// with RecordQuality.
func (s *Service) QualityResults(ctx context.Context) (contracts.QualityResults, error) {
	if s.Settings != nil {
		if on, err := s.Settings.GetBool(ctx, SettingQuality, true); err == nil && !on {
			return contracts.QualityResults{Off: true, Results: []contracts.QualityResult{}}, nil
		}
	}
	if _, kept := qualityCache.Get("results"); !kept && s.RecordQuality != nil {
		url := s.Client.QualityURL
		if url == "" {
			url = DefaultQualityURL
		}
		s.RecordQuality(ctx, url)
	}
	return s.Client.QualityResults(ctx)
}
