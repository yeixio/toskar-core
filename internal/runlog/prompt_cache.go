package runlog

import (
	"sort"
	"time"
)

// PromptCache is how much of each chat turn's prompt the models on this
// computer read from their prompt cache instead of processing again (#838),
// over chats' later turns. A model that reuses little re-reads a whole long
// chat every turn, which is slow; models with sliding-window attention can
// do this.
type PromptCache struct {
	Since  time.Time          `json:"since"`
	Models []PromptCacheModel `json:"models"`
}

// PromptCacheModel is one model's later turns.
type PromptCacheModel struct {
	ModelID string `json:"model_id"`
	// Turns counts the later turns of chats the model answered, already
	// running.
	Turns        int `json:"turns"`
	PromptTokens int `json:"prompt_tokens"`
	CachedTokens int `json:"cached_tokens"`
	// Reuse is CachedTokens over PromptTokens, 0 to 1.
	Reuse float64 `json:"reuse"`
	// Low is set when, over at least PromptCacheMinTurns turns, under
	// PromptCacheLowReuse of the prompt came from the cache.
	Low bool `json:"low,omitempty"`
}

// A model is flagged when fewer than half its later turns' prompts came from
// the cache, over enough turns to say. The weekly quality run holds its
// model to 80% (tests/quality/cache_test.go); a person's chats mix in
// other chats between turns, which can push a turn out of the cache.
const (
	PromptCacheLowReuse = 0.5
	PromptCacheMinTurns = 5
)

// SummarizePromptCache adds up, per model, the prompt and cached tokens of
// the completed runs that weren't the first in their chat within the
// window. A model counts only where it ran on this computer (it has a
// backend, as llama.cpp reports) and was already running: other runtimes
// and paired computers don't report their cache, and a model that just
// loaded starts cold. Models are ordered by turns, most first.
func SummarizePromptCache(since time.Time, runs []Run) PromptCache {
	byChat := map[string][]Run{}
	for _, r := range runs {
		if r.Status == StatusCompleted && r.ConversationID != "" {
			byChat[r.ConversationID] = append(byChat[r.ConversationID], r)
		}
	}
	by := map[string]*PromptCacheModel{}
	for _, chat := range byChat {
		sort.Slice(chat, func(i, j int) bool { return chat[i].StartedAt.Before(chat[j].StartedAt) })
		for _, r := range chat[1:] {
			for _, m := range r.Models {
				if m.Backend == "" || m.LoadMs > 0 || m.PromptTokens <= 0 {
					continue
				}
				p := by[m.ModelID]
				if p == nil {
					p = &PromptCacheModel{ModelID: m.ModelID}
					by[m.ModelID] = p
				}
				p.Turns++
				p.PromptTokens += m.PromptTokens
				p.CachedTokens += min(m.CachedTokens, m.PromptTokens)
			}
		}
	}
	out := PromptCache{Since: since.UTC(), Models: []PromptCacheModel{}}
	for _, p := range by {
		p.Reuse = float64(p.CachedTokens) / float64(p.PromptTokens)
		p.Low = p.Turns >= PromptCacheMinTurns && p.Reuse < PromptCacheLowReuse
		out.Models = append(out.Models, *p)
	}
	sort.Slice(out.Models, func(i, j int) bool {
		a, b := out.Models[i], out.Models[j]
		if a.Turns != b.Turns {
			return a.Turns > b.Turns
		}
		return a.ModelID < b.ModelID
	})
	return out
}
