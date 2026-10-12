package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/api"
	"github.com/yeixio/toskar-core/internal/relayclient"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// The outside check (#546, docs/remote-access.md): the relay reaches this
// computer from the internet, through its tunnel and each direct way in,
// the way a paired device away from home does. API Access shows the result,
// so an Admin can tell whether access from anywhere works, and when it
// doesn't, which way fails and why, instead of trusting what the router
// said when it opened a port.

const (
	// reachCheckEvery is how often the check runs on its own.
	reachCheckEvery = 30 * time.Minute
	// reachCheckSettle is how long after the relay starts, or the ways in
	// change, the check waits, so the tunnel and the router port are up.
	reachCheckSettle = 20 * time.Second
)

var errReachOff = contracts.NewError("REMOTE_ACCESS_OFF", nil, errors.New("access from anywhere is off, or its listener isn't up, so there's nothing to check"))

type reachChecker struct {
	mu   sync.Mutex
	last *relayclient.CheckResult
	// tried is the last attempt, and failed its code when it didn't reach
	// the relay; the last result stays shown.
	tried  time.Time
	failed string
	// soon is when a change asks for a check, zero for none.
	soon time.Time
}

// reachSoon asks for a check once things settle, as after the relay starts.
func (a *App) reachSoon() {
	a.reach.mu.Lock()
	a.reach.soon = time.Now().Add(reachCheckSettle)
	a.reach.mu.Unlock()
}

// checkReach runs the check now.
func (a *App) checkReach(ctx context.Context) error {
	a.relayMu.Lock()
	c := a.relay
	a.relayMu.Unlock()
	if c == nil {
		return errReachOff
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	res, err := c.Check(ctx)
	a.reach.mu.Lock()
	defer a.reach.mu.Unlock()
	a.reach.tried, a.reach.soon = time.Now(), time.Time{}
	if err != nil {
		a.reach.failed = relayclient.CheckCode(err)
		return nil
	}
	a.reach.last, a.reach.failed = &res, ""
	return nil
}

// reachLoop runs the check every reachCheckEvery, and after changes.
func (a *App) reachLoop(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			a.reach.mu.Lock()
			due := (!a.reach.soon.IsZero() && now.After(a.reach.soon)) || now.Sub(a.reach.tried) >= reachCheckEvery
			a.reach.mu.Unlock()
			if due {
				_ = a.checkReach(ctx)
			}
		}
	}
}

// reachCheck is the last check for Settings; nil before the first one.
func (a *App) reachCheck() *api.ReachCheck {
	a.reach.mu.Lock()
	defer a.reach.mu.Unlock()
	if a.reach.last == nil && a.reach.failed == "" {
		return nil
	}
	out := &api.ReachCheck{Tried: a.reach.tried, Error: a.reach.failed}
	if r := a.reach.last; r != nil {
		out.At = r.At
		out.Relay = &api.ReachWay{OK: r.Relay.OK, Reason: r.Relay.Reason, MS: r.Relay.MS}
		for _, d := range r.Direct {
			out.Direct = append(out.Direct, api.ReachWay{Address: d.Address, OK: d.OK, Reason: d.Reason, MS: d.MS})
		}
	}
	return out
}
