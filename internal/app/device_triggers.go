package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/connectors"
)

// Device triggers (#586): while an enabled automation has one, Toskar keeps
// a live watch on its connected service, which tells it of each change to
// the devices those automations name, and a matching change runs them at
// once. The watch follows the automations: it starts, changes, and stops
// as they do, and connects again after a drop.

// deviceRecheck is how often the watched devices are read again, and
// deviceBackoff how long the first reconnect waits, doubling to
// deviceBackoffMax, so connecting the service later is noticed soon.
var (
	deviceRecheck    = 15 * time.Second
	deviceBackoff    = 5 * time.Second
	deviceBackoffMax = 5 * time.Minute
)

// homeAssistant is the connector with devices to watch so far.
const homeAssistant = "homeassistant"

type deviceTriggers struct {
	app *App
	mu  sync.Mutex
	// last is when each automation's device trigger last ran it.
	last map[string]time.Time
	now  func() time.Time
}

// watchedDevices are the devices enabled automations' device triggers name,
// by connector, sorted.
func (d *deviceTriggers) watchedDevices(ctx context.Context) (map[string][]string, error) {
	list, err := d.app.Automations.DeviceTriggered(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, a := range list {
		t := a.Trigger
		if !slices.Contains(out[t.Connector], t.Device) {
			out[t.Connector] = append(out[t.Connector], t.Device)
		}
	}
	for k := range out {
		slices.Sort(out[k])
	}
	return out, nil
}

// loop keeps the watch on what the automations name, until ctx ends.
func (d *deviceTriggers) loop(ctx context.Context) {
	ctx = auth.WithSystem(ctx)
	var (
		watching map[string][]string
		cancel   context.CancelFunc = func() {}
		dropped                     = make(chan error, 1)
		backoff                     = deviceBackoff
		retryAt  time.Time
	)
	defer func() { cancel() }()
	tick := time.NewTicker(deviceRecheck)
	defer tick.Stop()
	for {
		want, err := d.watchedDevices(ctx)
		// A change in what's watched restarts the watch at once; after a
		// drop, it waits out the backoff.
		if err == nil && !sameDevices(want, watching) && (watching != nil || !time.Now().Before(retryAt)) {
			cancel()
			watching, cancel = nil, func() {}
			if devices := want[homeAssistant]; len(devices) > 0 {
				wctx, stop := context.WithCancel(ctx)
				watching, cancel = want, stop
				go func() {
					err := d.app.Connectors.WatchDevices(wctx, homeAssistant, devices, func(c connectors.DeviceChange) {
						d.changed(ctx, homeAssistant, c)
					})
					if wctx.Err() == nil {
						dropped <- err
					}
				}()
			}
		}
		select {
		case <-ctx.Done():
			return
		case err := <-dropped:
			// The watch dropped: try again after a wait that grows.
			if d.app.Logger != nil {
				d.app.Logger.Warn("device watch dropped", "error", err, "retry_in", backoff)
			}
			watching, retryAt = nil, time.Now().Add(backoff)
			backoff = min(backoff*2, deviceBackoffMax)
		case <-tick.C:
			if watching != nil {
				backoff = deviceBackoff
			}
		}
	}
}

func sameDevices(a, b map[string][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if !slices.Equal(v, b[k]) {
			return false
		}
	}
	return true
}

// changed runs the automations a device change matches: theirs, to the
// state they wait for, and not within their cooldown.
func (d *deviceTriggers) changed(ctx context.Context, connector string, c connectors.DeviceChange) {
	list, err := d.app.Automations.DeviceTriggered(ctx)
	if err != nil {
		return
	}
	for _, a := range list {
		if !deviceMatches(*a.Trigger, connector, c) || !d.due(a.ID, a.Trigger.DeviceCooldown()) {
			continue
		}
		_, err := d.app.AutomationRunner.RunWith(ctx, a.ID, automations.Found{Changed: true, Summary: deviceNote(c)})
		if err != nil && !errors.Is(err, automations.ErrRunning) && d.app.Logger != nil {
			d.app.Logger.Warn("device trigger run failed to start", "automation", a.ID, "error", err)
		}
	}
}

// deviceMatches reports a change a device trigger waits for.
func deviceMatches(t automations.Trigger, connector string, c connectors.DeviceChange) bool {
	return t.Kind == automations.TriggerDevice && t.Connector == connector && t.Device == c.Device &&
		(t.To == "" || strings.EqualFold(t.To, c.To))
}

// due reports an automation past its cooldown, and starts the next one.
func (d *deviceTriggers) due(id string, cooldown time.Duration) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	if last, ok := d.last[id]; ok && now.Sub(last) < cooldown {
		return false
	}
	d.last[id] = now
	return true
}

// deviceNote tells the run what changed.
func deviceNote(c connectors.DeviceChange) string {
	return fmt.Sprintf("The device %s changed from %q to %q at %s.", c.Device, c.From, c.To, c.At.Format("15:04:05"))
}
