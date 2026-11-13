package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/yeixio/toskar-core/internal/api/openai"
	"github.com/yeixio/toskar-core/internal/egress"
	"github.com/yeixio/toskar-core/internal/nodes"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/runtimes/external"
	"github.com/yeixio/toskar-core/internal/scheduler"
	"github.com/yeixio/toskar-core/internal/swarm"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Spread (#1135, docs/features/swarms.md#spread-editor-agents-subagents): an
// editor agent's subagents send swarm:<profile> requests with their own
// tools at once. Each goes, as it came, to the least busy of the profile's
// swarm models and computers that fits: Norn scores the computers that have
// each model by fit and load, and a computer and model with a free slot
// comes first. With every one busy, the request waits for the shortest
// line. A model on a computer answers as many at once as it has slots
// (App.swarmSlots).

// spreadTarget is one computer and model a Spread request can run on.
type spreadTarget struct {
	Node    contracts.Node
	ModelID string
	// Score is Norn's, for this model on this computer.
	Score int
}

func (t spreadTarget) key() laneKey { return laneKey{t.Node.ID, t.ModelID} }

type laneKey struct{ node, model string }

// spreadLanes are the Spread requests on each computer and model: running,
// at most its slots, and waiting for one. The zero value is ready.
type spreadLanes struct {
	mu      sync.Mutex
	running map[laneKey]int
	waiting map[laneKey]int
	// changed closes when a request finishes, for those waiting.
	changed chan struct{}
	// slots is how many requests a model on a computer answers at once;
	// nil is one.
	slots swarm.SlotsFor
}

// slotsOf is how many requests k answers at once.
func (l *spreadLanes) slotsOf(k laneKey) int {
	if l.slots == nil {
		return 1
	}
	return max(1, l.slots(k.node, k.model))
}

func (l *spreadLanes) init() {
	if l.running == nil {
		l.running, l.waiting, l.changed = map[laneKey]int{}, map[laneKey]int{}, make(chan struct{})
	}
}

// choose picks the target for a request and puts it in that line: one with
// a free slot, the best scored; else the one with the shortest line for
// its slots. The caller acquires a slot next, or leaves the line.
func (l *spreadLanes) choose(targets []spreadTarget) spreadTarget {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.init()
	type ranked struct {
		t           spreadTarget
		ahead, free int
	}
	all := make([]ranked, len(targets))
	for i, t := range targets {
		k := t.key()
		ahead := l.running[k] + l.waiting[k]
		all[i] = ranked{t: t, ahead: ahead, free: l.slotsOf(k) - ahead}
	}
	sort.SliceStable(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if (a.free > 0) != (b.free > 0) {
			return a.free > 0
		}
		if a.free <= 0 && a.free != b.free {
			// All busy: the shortest line for its slots.
			return a.free > b.free
		}
		if a.t.Score != b.t.Score {
			return a.t.Score > b.t.Score
		}
		return a.ahead < b.ahead
	})
	pick := all[0].t
	l.waiting[pick.key()]++
	return pick
}

// acquire waits for a slot on k, which choose put the request in line for,
// and takes it; or leaves the line when ctx ends.
func (l *spreadLanes) acquire(ctx context.Context, k laneKey) error {
	for {
		l.mu.Lock()
		l.init()
		if l.running[k] < l.slotsOf(k) {
			l.running[k]++
			l.waiting[k]--
			l.mu.Unlock()
			return nil
		}
		changed := l.changed
		l.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			l.mu.Lock()
			l.waiting[k]--
			l.mu.Unlock()
			return ctx.Err()
		}
	}
}

// release gives back a slot on k and wakes those waiting.
func (l *spreadLanes) release(k laneKey) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.running[k]--; l.running[k] <= 0 {
		delete(l.running, k)
	}
	close(l.changed)
	l.changed = make(chan struct{})
}

// spreadRoute readies a Spread request on the profile named by ref: it
// waits for a slot on the target chosen, readies the model there, and
// returns where the request goes. A target that can't take it, such as one
// whose window can't hold it, gives way to the next.
func (a *App) spreadRoute(ctx context.Context, ref string, need int) (openai.RawRoute, error) {
	profile, err := a.swarmProfile(ctx, ref)
	if err != nil {
		return openai.RawRoute{}, err
	}
	find := a.spreadTargets
	if a.spreadFind != nil {
		find = a.spreadFind
	}
	targets, err := find(ctx, profile, need)
	if err != nil {
		return openai.RawRoute{}, err
	}
	if len(targets) == 0 {
		return openai.RawRoute{}, fmt.Errorf("%w: none of the profile's swarm models is installed on a computer that's on; set its swarm models in Profiles, or install one", openai.ErrRawNotHere)
	}
	ready := a.spreadReady
	if a.spreadOpen != nil {
		ready = a.spreadOpen
	}
	var last error
	for len(targets) > 0 {
		t := a.spread.choose(targets)
		k := t.key()
		if err := a.spread.acquire(ctx, k); err != nil {
			return openai.RawRoute{}, err
		}
		var once sync.Once
		release := func() { once.Do(func() { a.spread.release(k) }) }
		route, err := ready(ctx, t, need)
		if err != nil {
			release()
			last = err
			if ctx.Err() != nil {
				return openai.RawRoute{}, err
			}
			targets = slices.DeleteFunc(targets, func(o spreadTarget) bool { return o.key() == k })
			continue
		}
		// Placement sees the request as work on that computer, as a chat's.
		work := a.beginWorkOf(t.Node.ID, need)
		done := route.Done
		route.Done = func() {
			if done != nil {
				done()
			}
			work()
			release()
		}
		route.NodeID, route.ModelID = t.Node.ID, t.ModelID
		if a.Logger != nil {
			a.Logger.Info("spread request placed", "profile", profile.ID, "node_id", t.Node.ID, "node", nodeDisplayName(t.Node), "model", t.ModelID, "need", need)
		}
		return route, nil
	}
	return openai.RawRoute{}, last
}

// spreadModels are the models a profile's Spread requests may run on: its
// swarm models, else its roles' models. Models in another service aren't
// raw models on a computer.
func spreadModels(p profiles.Profile) []string {
	var out []string
	add := func(id string) {
		if id = strings.TrimSpace(id); id != "" && !external.IsModel(id) && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	if p.Orchestration.Swarm != nil {
		for _, id := range p.Orchestration.Swarm.Models {
			add(id)
		}
	}
	if len(out) == 0 {
		for _, r := range p.Roles {
			add(r.ModelID)
		}
	}
	return out
}

// spreadTargets are the computers and models that can take a Spread request
// on profile: each of its models on each computer that's on, has it
// installed, and the profile allows, as Norn scores it, by fit and load.
func (a *App) spreadTargets(ctx context.Context, profile profiles.Profile, need int) ([]spreadTarget, error) {
	models := spreadModels(profile)
	if len(models) == 0 {
		return nil, fmt.Errorf("%w: the profile has no models; set its swarm models in Profiles", openai.ErrRawNotHere)
	}
	if a.Nodes == nil {
		return nil, errors.New("computers aren't available")
	}
	a.Nodes.RefreshPairedLivenessIfStale(ctx)
	nodeList, err := a.Nodes.List(ctx)
	if err != nil {
		return nil, err
	}
	installed, fit := a.spreadInstalled(ctx, nodeList, models)
	candidates := scheduler.BuildCandidates(nodeList, installed, nil)
	byID := map[string]contracts.Node{}
	for _, n := range nodeList {
		byID[n.ID] = n
	}
	active, inFlight := a.activeWork(), a.inFlightTokens()
	var out []spreadTarget
	for _, m := range models {
		for _, s := range scheduler.Score(scheduler.ScoreInput{
			Role: profiles.RoleAgent, ModelID: m, Profile: profile, Nodes: candidates,
			RequireModel: true, ActiveTasks: active, Training: a.Nodes.TrainingNodes(),
			Speeds: &a.speeds, PromptTokens: need, InFlightTokens: inFlight,
		}) {
			// A model that can't run on that computer isn't a target.
			if s.Score < -100 || !fit[laneKey{s.NodeID, m}] {
				continue
			}
			out = append(out, spreadTarget{Node: byID[s.NodeID], ModelID: m, Score: s.Score})
		}
	}
	return out, nil
}

// spreadInstalled lists, once per computer, which of models each one that's
// on has installed, and which of those fit it.
func (a *App) spreadInstalled(ctx context.Context, nodeList []contracts.Node, models []string) (map[string][]string, map[laneKey]bool) {
	installed := map[string][]string{}
	fit := map[laneKey]bool{}
	note := func(nodeID string, list []contracts.Model) {
		for _, m := range list {
			if !m.Installed || !slices.Contains(models, m.ID) {
				continue
			}
			installed[nodeID] = append(installed[nodeID], m.ID)
			fit[laneKey{nodeID, m.ID}] = m.Fit == nil || (m.Fit.Label != contracts.FitTooLarge && m.Fit.Label != contracts.FitUnsupported)
		}
	}
	for _, n := range nodeList {
		switch {
		case n.IsLocal:
			if a.Models != nil {
				if list, err := a.Models.List(ctx); err == nil {
					note(n.ID, list)
				}
			}
		case n.Paired && n.Address != "" && (n.Status == "" || n.Status == contracts.NodeStatusOnline):
			if list, err := a.peerClient(n).ListModels(ctx); err == nil {
				note(n.ID, list)
			}
		}
	}
	return installed, fit
}

// spreadReady readies a Spread request's model on its target: this
// computer's runtime, as a raw request's, or a paired computer's, reached
// over the same signed link Generate uses.
func (a *App) spreadReady(ctx context.Context, t spreadTarget, need int) (openai.RawRoute, error) {
	if t.Node.IsLocal || t.Node.ID == a.Config.Get().NodeID {
		endpoint, window, done, err := a.rawModel(ctx, t.ModelID, need)
		if err != nil {
			return openai.RawRoute{}, err
		}
		return openai.RawRoute{
			Send: func(ctx context.Context, body []byte) (*http.Response, error) {
				return openai.SendRaw(ctx, endpoint, body, window)
			},
			Done: done,
		}, nil
	}
	n := t.Node
	client := a.peerClient(n)
	cfg := a.Config.Get()
	return openai.RawRoute{
		Send: func(ctx context.Context, body []byte) (*http.Response, error) {
			a.Egress.Add(ctx, egress.PairedComputer, nodeDisplayName(n), fmt.Sprintf("prompt and context for %s", t.ModelID))
			resp, err := client.Raw(ctx, nodes.RemoteRawRequest{
				ModelID: t.ModelID, Need: need, Body: body,
				RequesterNodeID: cfg.NodeID, RequesterNodeName: cfg.NodeName,
			})
			if err == nil {
				return resp, nil
			}
			switch code, _ := contracts.ErrorCode(err); code {
			case "MODEL_NOT_FOUND":
				return nil, fmt.Errorf("%w: %s on %s", openai.ErrRawNotHere, t.ModelID, nodeDisplayName(n))
			case "CONTEXT_TOO_LONG":
				return nil, fmt.Errorf("%w: %s", openai.ErrRawTooLong, err.Error())
			}
			return nil, remoteUnreachableErr(n, err)
		},
		Done: func() {},
	}, nil
}

// internalRaw serves a paired computer's Spread request: the raw body goes
// to this computer's runtime for the model, as a raw request here would,
// and the answer goes back as it comes.
func (a *App) internalRaw(ctx context.Context, req nodes.RemoteRawRequest) (*http.Response, error) {
	endpoint, window, done, err := a.rawModel(ctx, req.ModelID, req.Need)
	switch {
	case errors.Is(err, openai.ErrRawNotHere):
		return nil, contracts.NewError("MODEL_NOT_FOUND", nil, fmt.Errorf("%s isn't installed on this computer", req.ModelID))
	case errors.Is(err, openai.ErrRawTooLong):
		return nil, contracts.NewError("CONTEXT_TOO_LONG", nil, err)
	case err != nil:
		return nil, err
	}
	resp, err := openai.SendRaw(ctx, endpoint, req.Body, window)
	if err != nil {
		done()
		return nil, err
	}
	resp.Body = &doneBody{ReadCloser: resp.Body, done: done}
	return resp, nil
}

// doneBody calls done once its body is closed.
type doneBody struct {
	io.ReadCloser
	once sync.Once
	done func()
}

func (b *doneBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.done)
	return err
}

// swarmProfile is the profile a swarm:<profile> request names: by id, or
// by its name as a slug, such as "code-review" for "Code review".
func (a *App) swarmProfile(ctx context.Context, ref string) (profiles.Profile, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		ref = a.defaultProfileID(ctx)
	}
	if p, err := a.Profiles.Get(ctx, ref); err == nil {
		return p, nil
	}
	list, err := a.Profiles.List(ctx)
	if err != nil {
		return profiles.Profile{}, err
	}
	for _, p := range list {
		if profileSlug(p.Name) == strings.ToLower(ref) {
			return p, nil
		}
	}
	return profiles.Profile{}, openai.ErrSwarmProfile
}

// profileSlug is a profile's name as a model name takes it: lower case,
// with dashes between words.
func profileSlug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
			continue
		}
		dash = true
	}
	return b.String()
}
