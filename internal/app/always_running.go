package app

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/yeixio/toskar-core/internal/share"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// alwaysRunningKey is the setting that lists the models kept loaded on this
// computer (#966): loaded when Toskar starts and again if one stops, and
// never unloaded by the idle sweep or to make room for a picture.
const alwaysRunningKey = "always_running_models"

// keepPinnedEvery is how often the keeper checks that each model kept
// running is loaded, such as after one crashed.
const keepPinnedEvery = time.Minute

func (a *App) alwaysRunningList(ctx context.Context) []string {
	if a.Settings == nil {
		return nil
	}
	raw, ok, err := a.Settings.Get(ctx, alwaysRunningKey)
	if err != nil || !ok || raw == "" {
		return nil
	}
	var ids []string
	if json.Unmarshal([]byte(raw), &ids) != nil {
		return nil
	}
	return ids
}

// alwaysRunning reports a model kept loaded on this computer.
func (a *App) alwaysRunning(ctx context.Context, modelID string) bool {
	return slices.Contains(a.alwaysRunningList(ctx), modelID)
}

// setAlwaysRunning keeps a chat model installed on this computer loaded, or
// lets the idle sweep unload it again. Turning it on loads it now.
func (a *App) setAlwaysRunning(ctx context.Context, modelID string, on bool) error {
	m, err := a.Models.Get(ctx, modelID)
	if err != nil || !m.Installed {
		return fmt.Errorf("model %s isn't installed on this computer", modelID)
	}
	if contracts.SupportRoleOf(m) != "" {
		return fmt.Errorf("model %s doesn't chat, so it isn't kept running", modelID)
	}
	ids := slices.DeleteFunc(a.alwaysRunningList(ctx), func(id string) bool { return id == modelID })
	if on {
		ids = append(ids, modelID)
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	if err := a.Settings.Set(ctx, alwaysRunningKey, string(raw)); err != nil {
		return err
	}
	a.pinStopped.Delete(modelID)
	if on {
		go a.keepPinnedLoaded(context.WithoutCancel(ctx))
	}
	return nil
}

// withAlwaysRunning marks the models kept loaded on this computer.
func (a *App) withAlwaysRunning(ctx context.Context, list []contracts.Model) {
	pinned := a.alwaysRunningList(ctx)
	for i, m := range list {
		if m.Installed && slices.Contains(pinned, m.ID) {
			list[i].AlwaysRunning = true
		}
	}
}

// keepPinnedLoaded loads each model kept running that isn't: at start, and
// after one stopped, such as by a crash. One the person stopped stays
// stopped until they start it or Toskar starts again, and a training run,
// which needs the memory, is waited out.
func (a *App) keepPinnedLoaded(ctx context.Context) {
	if a.Runtimes == nil || a.stubInference {
		return
	}
	if a.Share != nil {
		if _, training := a.Share.Running(share.Training); training {
			return
		}
	}
	pinned := a.alwaysRunningList(ctx)
	if len(pinned) == 0 {
		return
	}
	views, _ := a.localRunningViews(ctx)
	running := make([]string, len(views))
	for i, v := range views {
		running[i] = v.ModelID
	}
	stopped := func(id string) bool { _, ok := a.pinStopped.Load(id); return ok }
	for _, id := range pinnedToLoad(pinned, running, stopped) {
		if _, loading := a.pinLoading.LoadOrStore(id, struct{}{}); loading {
			continue
		}
		func() {
			defer a.pinLoading.Delete(id)
			if _, err := a.startModelLocal(ctx, id); err != nil && a.Logger != nil {
				a.Logger.Warn("always running model didn't load", "model", id, "error", err)
			}
		}()
	}
}

// pinnedToLoad is the models kept running that need loading: those not
// running, other than one the person stopped.
func pinnedToLoad(pinned, running []string, stopped func(string) bool) []string {
	var out []string
	for _, id := range pinned {
		if !slices.Contains(running, id) && !stopped(id) {
			out = append(out, id)
		}
	}
	return out
}

// runPinnedKeeper keeps the models kept running loaded until ctx ends.
func (a *App) runPinnedKeeper(ctx context.Context) {
	a.keepPinnedLoaded(ctx)
	ticker := time.NewTicker(keepPinnedEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.keepPinnedLoaded(ctx)
		}
	}
}

// stoppedByPerson notes a model kept running that the person stopped, so
// the keeper leaves it until they start it again.
func (a *App) stoppedByPerson(ctx context.Context, instanceID string) {
	views, _ := a.localRunningViews(ctx)
	for _, v := range views {
		if v.InstanceID == instanceID && a.alwaysRunning(ctx, v.ModelID) {
			a.pinStopped.Store(v.ModelID, struct{}{})
		}
	}
}
