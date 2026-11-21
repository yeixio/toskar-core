package portmap

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"time"
)

// A permanent UPnP mapping, the only kind some older routers take, outlives
// Toskar when it crashes: nothing renews it, and nothing removes it
// (#1262). While one is kept, its outside port is noted in Keeper.Record;
// the next start removes it from the router, if the router still forwards
// that port to this computer under Toskar's name, and forgets the note.

// record is what the note holds.
type record struct {
	ExternalPort uint16 `json:"external_port"`
}

// note keeps or forgets the note for the mapping just made or renewed.
func (k *Keeper) note(m Mapping, err error) {
	if k.Record == "" {
		return
	}
	if err != nil || !m.permanent {
		k.forget()
		return
	}
	data, _ := json.Marshal(record{ExternalPort: m.External.Port()})
	_ = os.WriteFile(k.Record, data, 0o600)
}

// forget removes the note: the mapping is gone, or has a lease.
func (k *Keeper) forget() {
	if k.Record != "" {
		_ = os.Remove(k.Record)
	}
}

// Sweep removes a permanent mapping a crash left, when nothing keeps one
// now. Run it at start when access from anywhere doesn't map a port;
// Start sweeps on its own.
func (k *Keeper) Sweep(ctx context.Context) {
	k.mu.Lock()
	running := k.cancel != nil
	k.mu.Unlock()
	if running {
		return
	}
	k.sweepMu.Lock()
	defer k.sweepMu.Unlock()
	k.sweep(ctx)
}

func (k *Keeper) sweep(ctx context.Context) {
	if k.Record == "" {
		return
	}
	data, err := os.ReadFile(k.Record)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	var left record
	if err != nil || json.Unmarshal(data, &left) != nil || left.ExternalPort == 0 {
		k.forget()
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	g, err := discoverIGD(ctx)
	if err != nil {
		// Not on that router's network now: try again at the next start.
		return
	}
	if upnpOwnEntry(ctx, g, left.ExternalPort) {
		if upnpUnmap(ctx, g, left.ExternalPort) != nil {
			return
		}
	}
	k.forget()
}
