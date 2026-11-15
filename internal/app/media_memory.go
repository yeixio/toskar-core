package app

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A picture or clip takes most of this computer's memory (#541). Loaded
// chat models left in place crowd it until sd-cli fails, so before a job
// the models in its way are unloaded, and no model starts until the job
// ends. One job runs at a time, pictures and clips together.

// mediaHeadroom is memory left spare beside a job and the models kept.
const mediaHeadroom = 2 << 30

// mediaWaitForReplies is how long a job waits for a model it must unload
// to finish the answer it's writing.
const mediaWaitForReplies = 2 * time.Minute

// mediaHold is the picture or clip job running on this computer, if any.
type mediaHold struct {
	mu   sync.Mutex
	done chan struct{} // closed when the job ends; nil while none runs
	what string
}

// enter waits for any job running to end, then holds the computer for
// one making what ("picture" or "clip"). The function returned ends it.
func (h *mediaHold) enter(ctx context.Context, what string) (func(), error) {
	for {
		h.mu.Lock()
		if h.done == nil {
			done := make(chan struct{})
			h.done, h.what = done, what
			h.mu.Unlock()
			var once sync.Once
			return func() {
				once.Do(func() {
					h.mu.Lock()
					h.done, h.what = nil, ""
					h.mu.Unlock()
					close(done)
				})
			}, nil
		}
		done := h.done
		h.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-done:
		}
	}
}

// running is the job's end and what it makes, while one runs.
func (h *mediaHold) running() (<-chan struct{}, string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.done, h.what, h.done != nil
}

// loadedModel is a running model as freeing memory sees it.
type loadedModel struct {
	InstanceID string
	ModelID    string
	Memory     int64
	OnGPU      bool
	LastUsed   time.Time
}

// mediaUnloads picks the loaded models to stop before a job needing need
// bytes. On a computer with its own graphics memory, every model on the
// GPU goes: that memory is what the job runs short of, and how much each
// model holds there isn't known. Models in system or unified memory go
// least recently used first, until the rest, the job, and headroom fit in
// memory.
func mediaUnloads(loaded []loadedModel, need, memory int64, dedicatedGPU bool) []loadedModel {
	var stop, rest []loadedModel
	for _, m := range loaded {
		if dedicatedGPU && m.OnGPU {
			stop = append(stop, m)
		} else {
			rest = append(rest, m)
		}
	}
	if memory <= 0 {
		return stop
	}
	sort.SliceStable(rest, func(i, j int) bool { return rest[i].LastUsed.Before(rest[j].LastUsed) })
	used := int64(0)
	for _, m := range rest {
		used += m.Memory
	}
	for _, m := range rest {
		if used+need+mediaHeadroom <= memory {
			break
		}
		stop = append(stop, m)
		used -= m.Memory
	}
	return stop
}

// mediaRoom is imagegen.Engine.Room: it holds this computer for one job
// and unloads the models in its way.
func (a *App) mediaRoom(ctx context.Context, need int64, what string) (func(), error) {
	release, err := a.media.enter(ctx, what)
	if err != nil {
		return nil, err
	}
	a.freeForMedia(ctx, need)
	return release, nil
}

func (a *App) freeForMedia(ctx context.Context, need int64) {
	if a.Runtimes == nil || a.stubInference {
		return
	}
	views, err := a.localRunningViews(ctx)
	if err != nil || len(views) == 0 {
		return
	}
	loaded := make([]loadedModel, 0, len(views))
	for _, v := range views {
		// A model kept running stays; the job makes do with the rest
		// (#966).
		if a.alwaysRunning(ctx, v.ModelID) {
			continue
		}
		m := loadedModel{InstanceID: v.InstanceID, ModelID: v.ModelID, Memory: int64(v.MemoryBytes), OnGPU: v.Accelerator != "" && v.Accelerator != "CPU"}
		if v.LastUsedAt != nil {
			m.LastUsed = *v.LastUsedAt
		}
		loaded = append(loaded, m)
	}
	memory, dedicated := int64(0), false
	if hw, err := a.detectHardware(ctx); err == nil {
		memory = int64(hw.Memory.TotalBytes)
		dedicated = hasDedicatedGPU(hw)
	}
	stop := mediaUnloads(loaded, need, memory, dedicated)
	if len(stop) == 0 {
		return
	}
	a.waitForReplies(ctx, stop)
	for _, m := range stop {
		if err := a.stopModelLocal(ctx, m.InstanceID); err != nil {
			if a.Logger != nil {
				a.Logger.Warn("unload for a picture or clip", "model_id", m.ModelID, "error", err)
			}
			continue
		}
		if a.Bus != nil {
			a.Bus.Publish(events.New(events.ModelUnloaded, map[string]any{
				"model_id": m.ModelID, "instance_id": m.InstanceID, "reason": "media",
			}))
		}
	}
}

// hasDedicatedGPU reports a graphics card with memory of its own.
func hasDedicatedGPU(hw contracts.HardwareInventory) bool {
	for _, ac := range hw.Accelerators {
		if ac.Kind == "gpu" && ac.DedicatedVRAM > 0 && !softwareAdapter(ac.Model) {
			return true
		}
	}
	return false
}

// waitForReplies waits, for a while, until none of models is writing an
// answer, so unloading one doesn't cut a reply off.
func (a *App) waitForReplies(ctx context.Context, models []loadedModel) {
	if a.Health == nil {
		return
	}
	deadline := time.Now().Add(mediaWaitForReplies)
	for time.Now().Before(deadline) {
		generating := a.Health.Generating()
		busy := false
		for _, m := range models {
			if generating[m.ModelID] {
				busy = true
				break
			}
		}
		if !busy {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// waitForMedia holds a model start while a picture or clip is being made,
// unless the model is already running. It says why it waits.
func (a *App) waitForMedia(ctx context.Context, modelID string) error {
	done, what, ok := a.media.running()
	if !ok {
		return nil
	}
	if a.Runtimes != nil {
		if running, err := a.Runtimes.ListRunning(ctx, "llamacpp"); err == nil {
			for _, r := range running {
				if r.ModelID == modelID {
					return nil
				}
			}
		}
	}
	reason := "Waiting for a " + what + " to finish"
	if a.Logger != nil {
		a.Logger.Info("model start waiting", "model_id", modelID, "reason", reason)
	}
	if a.Bus != nil {
		a.Bus.Publish(events.New(EventWorkWaiting, map[string]any{"class": "media", "label": modelID, "reason": reason}))
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}
