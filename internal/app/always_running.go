package app

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/yeixio/toskar-core/internal/locale"
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
// lets the idle sweep unload it again. Turning it on loads it now. It
// returns the memory the models kept running leave for others, when this
// computer's memory is known, so the person sees what it costs.
func (a *App) setAlwaysRunning(ctx context.Context, modelID string, on bool) (left int64, err error) {
	m, err := a.Models.Get(ctx, modelID)
	if err != nil || !m.Installed {
		return 0, fmt.Errorf("model %s isn't installed on this computer", modelID)
	}
	if contracts.SupportRoleOf(m) != "" {
		return 0, fmt.Errorf("model %s doesn't chat, so it isn't kept running", modelID)
	}
	ids := slices.DeleteFunc(a.alwaysRunningList(ctx), func(id string) bool { return id == modelID })
	if on {
		ids = append(ids, modelID)
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		return 0, err
	}
	if err := a.Settings.Set(ctx, alwaysRunningKey, string(raw)); err != nil {
		return 0, err
	}
	a.pinStopped.Delete(modelID)
	if on {
		go a.keepPinnedLoaded(context.WithoutCancel(ctx))
	}
	return a.memoryLeftBesidePinned(ctx, ids), nil
}

// memoryLeftBesidePinned is this computer's memory less what the models
// kept running need, or -1 when this computer's memory isn't known.
func (a *App) memoryLeftBesidePinned(ctx context.Context, pinned []string) int64 {
	total := int64(a.memoryTotal(ctx))
	if total <= 0 {
		return -1
	}
	for _, id := range pinned {
		if m, err := a.Models.Get(ctx, id); err == nil {
			total -= int64(m.MemoryNeeded)
		}
	}
	return max(total, 0)
}

// noRoomBesidePinned reports that the models kept running on this computer
// leave too little memory for modelID, which isn't loaded, so placement
// sends it to another computer that has it when there is one (#966).
func (a *App) noRoomBesidePinned(ctx context.Context, modelID string) bool {
	pinned := a.alwaysRunningList(ctx)
	if len(pinned) == 0 || a.Models == nil {
		return false
	}
	m, err := a.Models.Get(ctx, modelID)
	if err != nil || !m.Installed || m.MemoryNeeded == 0 {
		return false
	}
	views, _ := a.localRunningViews(ctx)
	running := make([]string, len(views))
	for i, v := range views {
		running[i] = v.ModelID
	}
	loaded := pinnedBeside(modelID, pinned, running)
	if len(loaded) == 0 {
		return false
	}
	left := a.memoryLeftBesidePinned(ctx, loaded)
	return left >= 0 && uint64(left) < m.MemoryNeeded
}

// pinnedBeside is the models kept running that are loaded beside modelID,
// or none when modelID is loaded or kept running itself.
func pinnedBeside(modelID string, pinned, running []string) []string {
	if slices.Contains(pinned, modelID) || slices.Contains(running, modelID) {
		return nil
	}
	var out []string
	for _, id := range pinned {
		if slices.Contains(running, id) {
			out = append(out, id)
		}
	}
	return out
}

// keptRunningMemory is what to say when a model didn't fit because a
// model kept running holds the memory, and whether one does.
func (a *App) keptRunningMemory(lang string) (string, bool) {
	ctx := context.Background()
	views, _ := a.localRunningViews(ctx)
	for _, v := range views {
		if !a.alwaysRunning(ctx, v.ModelID) {
			continue
		}
		name := v.DisplayName
		if name == "" {
			name = v.ModelID
		}
		gb := fmt.Sprintf("%.1f", float64(v.MemoryBytes)/1e9)
		return locale.T(lang, "chat:errors.keptRunningMemory", map[string]any{"name": name, "gb": gb}), true
	}
	return "", false
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
