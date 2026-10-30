package scheduler

import "fmt"

// Prediction weights (#836). A computer predicted to take twice as long as
// the fastest loses slowerWeight points, about what having the model
// installed is worth; the loss stops at maxSlower so a slow computer that
// has the model still beats a fast one that would have to download it.
const (
	slowerWeight = 200
	maxSlower    = 400
	// cacheSlack is how much slower than the fastest the computer holding
	// a chat's cache may be predicted and still keep the chat: the
	// prediction already counts its cached tokens, and the slack covers
	// what the estimate misses.
	cacheSlack = 0.5
	cacheBonus = 80
)

// predict adds each candidate's predicted time to answer to its score:
// queue wait (tokens and answers in flight), the prompt tokens it doesn't
// have cached read at its prefill speed, and the expected answer at its
// decode speed. A computer with no speed measured for the model is left
// as it is. A follow-up stays on the computer that has the chat cached
// while that one is within cacheSlack of the fastest.
func predict(in ScoreInput, out []ScoredNode) {
	if in.ModelID == "" || len(out) == 0 {
		return
	}
	for i := range out {
		if t := in.InFlightTokens[out[i].NodeID]; t > 0 {
			out[i].Reasons = append(out[i].Reasons, fmt.Sprintf("%d tokens in flight", t))
		}
	}
	if in.Speeds == nil {
		affinity(in, out, nil, 0)
		return
	}
	expected := in.Speeds.ExpectedOutput(in.ModelID)
	times := map[string]float64{}
	best := 0.0
	for _, s := range out {
		prefill, decode, ok := in.Speeds.Rate(s.NodeID, in.ModelID)
		if !ok || prefill <= 0 || decode <= 0 {
			continue
		}
		queue := float64(in.InFlightTokens[s.NodeID])/prefill + float64(in.ActiveTasks[s.NodeID]*expected)/decode
		uncached := float64(in.PromptTokens) - cachedTokens(in, s.NodeID)
		if uncached < 0 {
			uncached = 0
		}
		t := queue + uncached/prefill + float64(expected)/decode
		times[s.NodeID] = t
		if best == 0 || t < best {
			best = t
		}
	}
	for i := range out {
		t, ok := times[out[i].NodeID]
		switch {
		case !ok:
			if len(times) > 0 {
				out[i].Reasons = append(out[i].Reasons, "no speed measured for this model yet")
			}
		case t <= best:
			out[i].Reasons = append(out[i].Reasons, fmt.Sprintf("answers in about %.1f s, the fastest", t))
		default:
			loss := int(slowerWeight * (t/best - 1))
			if loss > maxSlower {
				loss = maxSlower
			}
			out[i].Score -= loss
			out[i].Reasons = append(out[i].Reasons, fmt.Sprintf("answers in about %.1f s, %.1f s slower than the fastest", t, t-best))
		}
	}
	affinity(in, out, times, best)
}

// cachedTokens is how much of the request node is predicted to have in its
// prompt cache: the chat's earlier turns, when it served the last one.
func cachedTokens(in ScoreInput, node string) float64 {
	p := in.Previous
	if p == nil || p.NodeID != node || p.ModelID != in.ModelID {
		return 0
	}
	hit, _ := in.Speeds.CacheHit(node)
	return hit * float64(min(p.Tokens, in.PromptTokens))
}

// affinity keeps a follow-up on the computer with its cache, unless that
// one is predicted more than cacheSlack slower than the fastest.
func affinity(in ScoreInput, out []ScoredNode, times map[string]float64, best float64) {
	p := in.Previous
	if p == nil || p.NodeID == "" || p.ModelID != in.ModelID {
		return
	}
	for i := range out {
		if out[i].NodeID != p.NodeID {
			continue
		}
		if t, ok := times[p.NodeID]; ok && best > 0 && t > best*(1+cacheSlack) {
			out[i].Reasons = append(out[i].Reasons, "has this chat cached, but is too much slower now")
			return
		}
		out[i].Score += cacheBonus
		out[i].Reasons = append(out[i].Reasons, "has this chat cached")
		return
	}
}
