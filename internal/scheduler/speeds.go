package scheduler

import (
	"slices"
	"sync"
)

// speedWindow is how many recent answers a speed is the median of: enough
// that one slow answer doesn't move it, few enough that it follows an
// update or a change of GPU layers within a day's use.
const speedWindow = 32

// Sample is what one answer, or one benchmark run, measured (#836).
type Sample struct {
	PromptTokens     int
	CachedTokens     int
	CompletionTokens int
	// PrefillPerSec and DecodePerSec are llama-server's prompt and
	// generation speeds; zero when not reported.
	PrefillPerSec float64
	DecodePerSec  float64
	// FollowUp is set when the answer continued a chat on the computer that
	// served its previous turn, so its cached share measures cache hits.
	FollowUp bool
}

// Speeds keeps rolling medians of how fast each computer runs each model,
// from the answers Toskar has seen and from benchmarks (#836). It is safe
// for concurrent use; the zero value is ready.
type Speeds struct {
	mu      sync.Mutex
	rates   map[speedKey]*rateWindows
	outputs map[string]*window // model -> completion tokens
	hits    map[string]*window // node -> cached share of a follow-up's prompt
}

type speedKey struct{ node, model string }

type rateWindows struct{ prefill, decode window }

// window is the last speedWindow values, oldest first.
type window struct{ values []float64 }

func (w *window) add(v float64) {
	w.values = append(w.values, v)
	if len(w.values) > speedWindow {
		w.values = w.values[len(w.values)-speedWindow:]
	}
}

func (w *window) median() (float64, bool) {
	if w == nil || len(w.values) == 0 {
		return 0, false
	}
	s := slices.Clone(w.values)
	slices.Sort(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2], true
	}
	return (s[n/2-1] + s[n/2]) / 2, true
}

// Observe adds what an answer from model on node measured.
func (s *Speeds) Observe(node, model string, m Sample) {
	if s == nil || node == "" || model == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rates == nil {
		s.rates = map[speedKey]*rateWindows{}
		s.outputs = map[string]*window{}
		s.hits = map[string]*window{}
	}
	k := speedKey{node, model}
	r := s.rates[k]
	if r == nil {
		r = &rateWindows{}
		s.rates[k] = r
	}
	// A prompt read almost entirely from cache gives a speed for a handful
	// of tokens, which says little; leave it out.
	if m.PrefillPerSec > 0 && m.PromptTokens-m.CachedTokens >= 32 {
		r.prefill.add(m.PrefillPerSec)
	}
	if m.DecodePerSec > 0 && m.CompletionTokens >= 8 {
		r.decode.add(m.DecodePerSec)
	}
	if m.CompletionTokens > 0 {
		w := s.outputs[model]
		if w == nil {
			w = &window{}
			s.outputs[model] = w
		}
		w.add(float64(m.CompletionTokens))
	}
	if m.FollowUp && m.PromptTokens > 0 {
		w := s.hits[node]
		if w == nil {
			w = &window{}
			s.hits[node] = w
		}
		w.add(float64(m.CachedTokens) / float64(m.PromptTokens))
	}
}

// Rate is node's median prefill and decode speed for model, in tokens a
// second, and whether both have been measured.
func (s *Speeds) Rate(node, model string) (prefill, decode float64, ok bool) {
	if s == nil {
		return 0, 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rates[speedKey{node, model}]
	if r == nil {
		return 0, 0, false
	}
	p, okP := r.prefill.median()
	d, okD := r.decode.median()
	return p, d, okP && okD
}

// defaultOutput is the answer length assumed for a model with none seen.
const defaultOutput = 400

// ExpectedOutput is the median answer length of model, in tokens.
func (s *Speeds) ExpectedOutput(model string) int {
	if s == nil {
		return defaultOutput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.outputs[model].median(); ok {
		return int(v)
	}
	return defaultOutput
}

// defaultCacheHit is the share of a follow-up's prompt assumed to be in the
// cache of the computer that served the chat before, until it's measured:
// llama-server keeps the chat's earlier turns, so most of it is.
const defaultCacheHit = 0.9

// CacheHit is the median share of a follow-up's prompt node had cached,
// and whether it has been measured.
func (s *Speeds) CacheHit(node string) (float64, bool) {
	if s == nil {
		return defaultCacheHit, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.hits[node].median(); ok {
		return v, true
	}
	return defaultCacheHit, false
}
