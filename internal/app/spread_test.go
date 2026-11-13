package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/api/openai"
	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// fakeComputer is a computer's llama-server for one model: each request
// says it arrived, then waits for the test to let it answer.
type fakeComputer struct {
	name    string
	server  *httptest.Server
	arrived chan string
	proceed chan struct{}
}

func newFakeComputer(t *testing.T, name string, arrived chan string) *fakeComputer {
	t.Helper()
	c := &fakeComputer{name: name, arrived: arrived, proceed: make(chan struct{})}
	c.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		c.arrived <- c.name
		select {
		case <-c.proceed:
		case <-r.Context().Done():
			return
		case <-time.After(10 * time.Second):
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"model":"llama","choices":[{"message":{"role":"assistant","content":"from %s"},"finish_reason":"stop"}]}`, c.name)
	}))
	t.Cleanup(c.server.Close)
	return c
}

// spreadSetup is an app whose Spread targets are fake computers, each with
// one model, behind the real choice of target, slots, and lines, and an
// API handler for it.
func spreadSetup(t *testing.T, slots int, computers ...*fakeComputer) (*swarmApp, *openai.Handler, string) {
	t.Helper()
	s := newSwarmApp(t)
	s.SwarmSlots = func(string, string) int { return slots }
	ctx := context.Background()
	var models []string
	for _, c := range computers {
		models = append(models, "model-"+c.name)
	}
	p, err := s.Profiles.Create(ctx, profiles.Profile{Name: "Coding swarm", OrchestratorID: "simple",
		Roles:         []contracts.ModelRole{{Role: "assistant"}},
		Orchestration: contracts.OrchestrationPolicy{Strategy: profiles.StrategySwarm, Swarm: &contracts.SwarmPolicy{Models: models}}})
	if err != nil {
		t.Fatal(err)
	}
	byNode := map[string]*fakeComputer{}
	for _, c := range computers {
		byNode[c.name] = c
	}
	s.spreadFind = func(_ context.Context, profile profiles.Profile, _ int) ([]spreadTarget, error) {
		if !slices.Equal(spreadModels(profile), models) {
			t.Errorf("the profile's models = %v", spreadModels(profile))
		}
		var out []spreadTarget
		for _, c := range computers {
			out = append(out, spreadTarget{Node: contracts.Node{ID: c.name, Name: c.name, Paired: true}, ModelID: "model-" + c.name, Score: 200})
		}
		return out, nil
	}
	s.spreadOpen = func(_ context.Context, t spreadTarget, _ int) (openai.RawRoute, error) {
		url := byNode[t.Node.ID].server.URL
		return openai.RawRoute{
			Send: func(ctx context.Context, body []byte) (*http.Response, error) {
				return openai.SendRaw(ctx, url, body, 32768)
			},
			Done: func() {},
		}, nil
	}
	h := &openai.Handler{Profiles: s.Profiles, Spread: s.spreadRoute, Swarm: s.swarmTurn,
		Permissions: func(r *http.Request) (auth.APIKeyPermissions, context.Context, error) {
			return auth.APIKeyPermissions{Raw: true}, r.Context(), nil
		}}
	return s, h, p.ID
}

// spreadAsk sends a Spread request, an editor agent's subagent's with its
// own tools, and returns the answer when it comes.
func spreadAsk(h *openai.Handler, profile string) <-chan *httptest.ResponseRecorder {
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		body := `{"model":"swarm:` + profile + `","messages":[{"role":"user","content":"find the bug"}],"tools":[{"type":"function","function":{"name":"grep","parameters":{"type":"object"}}}]}`
		rec := httptest.NewRecorder()
		h.HandleChatCompletions(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
		done <- rec
	}()
	return done
}

func waitArrival(t *testing.T, arrived chan string, within time.Duration) (string, bool) {
	t.Helper()
	select {
	case who := <-arrived:
		return who, true
	case <-time.After(within):
		return "", false
	}
}

// The core claim of Spread (#1135): two of an editor agent's subagents'
// requests at once run at once on two computers, each with its model,
// instead of one waiting for the other; and with one computer and model of
// one slot, the second waits for the first.
func TestSpreadRunsAtOnce(t *testing.T) {
	arrived := make(chan string, 4)
	left, right := newFakeComputer(t, "left", arrived), newFakeComputer(t, "right", arrived)
	s, h, profile := spreadSetup(t, 1, left, right)

	first, second := spreadAsk(h, profile), spreadAsk(h, profile)
	// Both reach their computers while neither has answered: they overlap.
	a, ok1 := waitArrival(t, arrived, 5*time.Second)
	b, ok2 := waitArrival(t, arrived, 5*time.Second)
	if !ok1 || !ok2 || a == b {
		t.Fatalf("the two requests didn't run at once on two computers: %q, %q", a, b)
	}
	// Placement counts both as work on their computers while they run.
	if work := s.activeWork(); work["left"] != 1 || work["right"] != 1 {
		t.Fatalf("work = %v", work)
	}
	close(left.proceed)
	close(right.proceed)
	answers := map[bool]bool{}
	for _, done := range []<-chan *httptest.ResponseRecorder{first, second} {
		rec := <-done
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"model":"swarm:`+profile+`"`) {
			t.Fatalf("answer: %d %s", rec.Code, rec.Body)
		}
		answers[strings.Contains(rec.Body.String(), "from left")] = true
	}
	if len(answers) != 2 {
		t.Fatal("both answers came from one computer")
	}
	if work := s.activeWork(); len(work) != 0 {
		t.Fatalf("work after = %v", work)
	}
}

func TestSpreadQueuesOnOneTarget(t *testing.T) {
	arrived := make(chan string, 4)
	only := newFakeComputer(t, "only", arrived)
	_, h, profile := spreadSetup(t, 1, only)

	first, second := spreadAsk(h, profile), spreadAsk(h, profile)
	if _, ok := waitArrival(t, arrived, 5*time.Second); !ok {
		t.Fatal("the first request didn't arrive")
	}
	// One slot: the second waits in line while the first runs.
	if who, ok := waitArrival(t, arrived, 300*time.Millisecond); ok {
		t.Fatalf("a second request reached %s while the first ran", who)
	}
	only.proceed <- struct{}{}
	if _, ok := waitArrival(t, arrived, 5*time.Second); !ok {
		t.Fatal("the second request didn't run once the first finished")
	}
	only.proceed <- struct{}{}
	for _, done := range []<-chan *httptest.ResponseRecorder{first, second} {
		if rec := <-done; rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "from only") {
			t.Fatalf("answer: %d %s", rec.Code, rec.Body)
		}
	}
}

// A model with two slots takes two at once on one computer.
func TestSpreadUsesSlots(t *testing.T) {
	arrived := make(chan string, 4)
	only := newFakeComputer(t, "only", arrived)
	_, h, profile := spreadSetup(t, 2, only)
	first, second := spreadAsk(h, profile), spreadAsk(h, profile)
	for range 2 {
		if _, ok := waitArrival(t, arrived, 5*time.Second); !ok {
			t.Fatal("two slots didn't run two at once")
		}
	}
	close(only.proceed)
	<-first
	<-second
}

// The lines: a free slot first, then the best score; with all busy, the
// shortest line; and one who stops waiting leaves it.
func TestSpreadChoice(t *testing.T) {
	l := spreadLanes{slots: func(string, string) int { return 1 }}
	a := spreadTarget{Node: contracts.Node{ID: "a"}, ModelID: "m", Score: 300}
	b := spreadTarget{Node: contracts.Node{ID: "b"}, ModelID: "m", Score: 100}
	targets := []spreadTarget{b, a}
	if got := l.choose(targets); got.Node.ID != "a" {
		t.Fatalf("first = %s, want the best scored", got.Node.ID)
	}
	if err := l.acquire(context.Background(), a.key()); err != nil {
		t.Fatal(err)
	}
	if got := l.choose(targets); got.Node.ID != "b" {
		t.Fatalf("second = %s, want the free one", got.Node.ID)
	}
	if err := l.acquire(context.Background(), b.key()); err != nil {
		t.Fatal(err)
	}
	// Both busy: the third waits on the better one, the fourth on the other.
	if got := l.choose(targets); got.Node.ID != "a" {
		t.Fatalf("third = %s", got.Node.ID)
	}
	if got := l.choose(targets); got.Node.ID != "b" {
		t.Fatalf("fourth = %s, want the shorter line", got.Node.ID)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := l.acquire(ctx, b.key()); err == nil {
			t.Error("acquired a busy slot")
		}
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	wg.Wait()
	l.release(a.key())
	if err := l.acquire(context.Background(), a.key()); err != nil {
		t.Fatal(err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.waiting[b.key()] != 0 || l.waiting[a.key()] != 0 || l.running[a.key()] != 1 {
		t.Fatalf("lines = running %v waiting %v", l.running, l.waiting)
	}
}

// Spread models are the profile's swarm models, else its roles' models,
// and a profile is named by id or by its name as a slug.
func TestSpreadModelsAndProfiles(t *testing.T) {
	p := profiles.Profile{Roles: []contracts.ModelRole{{Role: "assistant", ModelID: "a"}, {Role: "fast", ModelID: "b"}, {Role: "planner", ModelID: "a"}}}
	if got := spreadModels(p); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("roles' models = %v", got)
	}
	p.Orchestration.Swarm = &contracts.SwarmPolicy{Models: []string{"c", "d"}}
	if got := spreadModels(p); !slices.Equal(got, []string{"c", "d"}) {
		t.Fatalf("swarm models = %v", got)
	}
	if got := profileSlug("Code review, fast!"); got != "code-review-fast" {
		t.Fatalf("slug = %q", got)
	}
	s := newSwarmApp(t)
	ctx := context.Background()
	made, err := s.Profiles.Create(ctx, profiles.Profile{Name: "Code review", OrchestratorID: "simple", Roles: []contracts.ModelRole{{Role: "assistant"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{made.ID, "code-review"} {
		if got, err := s.swarmProfile(ctx, ref); err != nil || got.ID != made.ID {
			t.Fatalf("%s: %+v, %v", ref, got, err)
		}
	}
	if _, err := s.swarmProfile(ctx, "nope"); err != openai.ErrSwarmProfile {
		t.Fatalf("unknown: %v", err)
	}
}
