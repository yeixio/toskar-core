package scheduler

import (
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// twoComputers are a Mac and a GPU box, both online with m1 installed and
// running, so only the prediction tells them apart.
func twoComputers(speeds *Speeds) ScoreInput {
	node := func(id string) NodeCandidate {
		return NodeCandidate{
			Node:            contracts.Node{ID: id, Status: contracts.NodeStatusOnline, Paired: true},
			InstalledModels: map[string]struct{}{"m1": {}},
		}
	}
	return ScoreInput{
		Role: "assistant", ModelID: "m1", Speeds: speeds,
		Nodes:         []NodeCandidate{node("mac"), node("gpu")},
		RunningModels: map[string]map[string]struct{}{"mac": {"m1": {}}, "gpu": {"m1": {}}},
	}
}

// measured gives node a few answers at these speeds.
func measured(s *Speeds, node string, prefill, decode float64) {
	for range 5 {
		s.Observe(node, "m1", Sample{PromptTokens: 1000, CompletionTokens: 400, PrefillPerSec: prefill, DecodePerSec: decode})
	}
}

func placedOn(t *testing.T, in ScoreInput) ScoredNode {
	t.Helper()
	d, err := Place(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range Score(in) {
		if s.NodeID == d.NodeID {
			return s
		}
	}
	t.Fatalf("placed on %s, which wasn't scored", d.NodeID)
	return ScoredNode{}
}

func reasonsOf(t *testing.T, in ScoreInput, node string) string {
	t.Helper()
	for _, s := range Score(in) {
		if s.NodeID == node {
			return strings.Join(s.Reasons, "; ")
		}
	}
	t.Fatalf("%s wasn't scored", node)
	return ""
}

func TestPlacesOnTheFasterComputer(t *testing.T) {
	s := &Speeds{}
	measured(s, "mac", 500, 40)
	measured(s, "gpu", 3000, 90)
	in := twoComputers(s)
	in.PromptTokens = 800
	if got := placedOn(t, in); got.NodeID != "gpu" {
		t.Fatalf("placed on %s: %v", got.NodeID, got.Reasons)
	}
	if r := reasonsOf(t, in, "gpu"); !strings.Contains(r, "the fastest") {
		t.Errorf("gpu reasons: %s", r)
	}
	if r := reasonsOf(t, in, "mac"); !strings.Contains(r, "s slower than the fastest") {
		t.Errorf("mac reasons: %s", r)
	}
}

// A computer that reads prompts fast but writes slowly wins long requests
// and loses short ones.
func TestLongAndShortRequestsGoWhereEachIsFaster(t *testing.T) {
	s := &Speeds{}
	measured(s, "mac", 400, 60)  // short: 0.5 s + 6.7 s; long: 50 s + 6.7 s
	measured(s, "gpu", 4000, 30) // short: 0.05 s + 13.3 s; long: 5 s + 13.3 s
	in := twoComputers(s)
	in.PromptTokens = 200
	if got := placedOn(t, in); got.NodeID != "mac" {
		t.Fatalf("short request on %s: %v", got.NodeID, got.Reasons)
	}
	in.PromptTokens = 20000
	if got := placedOn(t, in); got.NodeID != "gpu" {
		t.Fatalf("long request on %s: %v", got.NodeID, got.Reasons)
	}
}

func TestBalancesTokensInFlight(t *testing.T) {
	s := &Speeds{}
	measured(s, "mac", 1000, 50)
	measured(s, "gpu", 1000, 50)
	in := twoComputers(s)
	in.PromptTokens = 500
	// One turn each, but the GPU box's is a long one.
	in.ActiveTasks = map[string]int{"mac": 1, "gpu": 1}
	in.InFlightTokens = map[string]int{"mac": 300, "gpu": 30000}
	if got := placedOn(t, in); got.NodeID != "mac" {
		t.Fatalf("placed on %s: %v", got.NodeID, got.Reasons)
	}
	if r := reasonsOf(t, in, "gpu"); !strings.Contains(r, "30000 tokens in flight") {
		t.Errorf("gpu reasons: %s", r)
	}
}

func TestFollowUpStaysWithItsCache(t *testing.T) {
	s := &Speeds{}
	measured(s, "mac", 1000, 50)
	measured(s, "gpu", 1200, 50) // a little faster
	in := twoComputers(s)
	in.PromptTokens = 9000
	in.Previous = &PreviousTurn{NodeID: "mac", ModelID: "m1", Tokens: 8500}
	if got := placedOn(t, in); got.NodeID != "mac" {
		t.Fatalf("follow-up on %s: %v", got.NodeID, got.Reasons)
	}
	if r := reasonsOf(t, in, "mac"); !strings.Contains(r, "has this chat cached") {
		t.Errorf("mac reasons: %s", r)
	}

	// The cache isn't worth waiting behind a long queue.
	in.ActiveTasks = map[string]int{"mac": 3}
	in.InFlightTokens = map[string]int{"mac": 60000}
	if got := placedOn(t, in); got.NodeID != "gpu" {
		t.Fatalf("busy cache: placed on %s: %v", got.NodeID, got.Reasons)
	}
	if r := reasonsOf(t, in, "mac"); !strings.Contains(r, "too much slower") {
		t.Errorf("mac reasons: %s", r)
	}

	// Another model doesn't have the chat cached.
	in.ActiveTasks, in.InFlightTokens = nil, nil
	in.Previous.ModelID = "m2"
	if r := reasonsOf(t, in, "mac"); strings.Contains(r, "cached") {
		t.Errorf("other model counted as cached: %s", r)
	}
}

// With nothing measured, placement is as before, and a follow-up still
// prefers its cache.
func TestUnmeasuredComputersScoreAsBefore(t *testing.T) {
	in := twoComputers(&Speeds{})
	in.PromptTokens = 5000
	before := Score(twoComputers(nil))
	after := Score(in)
	for i := range before {
		if before[i].Score != after[i].Score {
			t.Fatalf("%s: %d without speeds, %d with none measured", before[i].NodeID, before[i].Score, after[i].Score)
		}
	}
	in.Previous = &PreviousTurn{NodeID: "gpu", ModelID: "m1", Tokens: 4000}
	if got := placedOn(t, in); got.NodeID != "gpu" {
		t.Fatalf("follow-up on %s", got.NodeID)
	}

	// One measured computer doesn't count against one that isn't.
	measured(in.Speeds, "mac", 100, 5)
	in.Previous = nil
	if r := reasonsOf(t, in, "gpu"); !strings.Contains(r, "no speed measured") {
		t.Errorf("gpu reasons: %s", r)
	}
}

func TestSpeedsAreRollingMedians(t *testing.T) {
	s := &Speeds{}
	if _, _, ok := s.Rate("mac", "m1"); ok {
		t.Fatal("rate before any answer")
	}
	for _, v := range []float64{50, 52, 900, 48, 51} { // one outlier
		s.Observe("mac", "m1", Sample{PromptTokens: 500, CompletionTokens: 100, PrefillPerSec: v * 10, DecodePerSec: v})
	}
	if _, d, ok := s.Rate("mac", "m1"); !ok || d != 51 {
		t.Fatalf("decode median %v, %v", d, ok)
	}
	// A prompt read from cache says nothing about prefill speed.
	s.Observe("gpu", "m1", Sample{PromptTokens: 500, CachedTokens: 490, CompletionTokens: 100, PrefillPerSec: 99999, DecodePerSec: 40})
	if _, _, ok := s.Rate("gpu", "m1"); ok {
		t.Fatal("prefill measured from a cached prompt")
	}
	// The window keeps the latest answers: an update that doubles speed
	// shows after a window of answers.
	for range speedWindow {
		s.Observe("mac", "m1", Sample{PromptTokens: 500, CompletionTokens: 100, PrefillPerSec: 1000, DecodePerSec: 100})
	}
	if _, d, _ := s.Rate("mac", "m1"); d != 100 {
		t.Fatalf("decode after update %v", d)
	}
	if n := s.ExpectedOutput("m1"); n != 100 {
		t.Fatalf("expected output %d", n)
	}
	if n := s.ExpectedOutput("unseen"); n != defaultOutput {
		t.Fatalf("expected output for an unseen model %d", n)
	}

	if hit, ok := s.CacheHit("mac"); ok || hit != defaultCacheHit {
		t.Fatalf("cache hit before follow-ups %v %v", hit, ok)
	}
	s.Observe("mac", "m1", Sample{PromptTokens: 1000, CachedTokens: 700, CompletionTokens: 50, FollowUp: true})
	if hit, ok := s.CacheHit("mac"); !ok || hit != 0.7 {
		t.Fatalf("cache hit %v %v", hit, ok)
	}
}
