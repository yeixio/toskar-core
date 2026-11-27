package app

import (
	"context"
	"slices"
	"sort"

	"github.com/yeixio/toskar-core/internal/events"
)

// A chat model that starts while other models fill the graphics card is
// put on the CPU instead, layer by layer, and answers many times slower: a
// 27B model on a 24 GB card answered at 2.6 words a second beside an idle
// 14B and a 4B, against 40 with the card to itself. So before a model
// starts, the idle models holding the card are unloaded, least recently
// used first, until it fits. Models kept running stay, as do models
// writing an answer or loading for one. A model kept only because a phone
// used it lately, or as the main chat model, gives way: the model about
// to answer matters more.

// gpuHeadroom is graphics memory left spare beside a model starting.
const gpuHeadroom = 1 << 30

// gpuModel is a running model as freeing the graphics card sees it.
type gpuModel struct {
	InstanceID string
	ModelID    string
	// GPU is what the model holds on the graphics card.
	GPU      uint64
	LastUsed int64 // unix nanoseconds; 0 when never used
	// Stays is a model that mustn't be unloaded for room.
	Stays bool
}

// gpuUnloads picks the models to unload so a model needing need bytes
// fits on a card of vram bytes. A model bigger than the card can't fit
// whole, so it gets as much of the card as can be freed. Nothing is
// unloaded when the card is unknown or the model already fits.
func gpuUnloads(running []gpuModel, need, vram uint64) []gpuModel {
	if vram == 0 || need == 0 {
		return nil
	}
	want := min(need, vram-min(vram, gpuHeadroom))
	used := uint64(0)
	var free []gpuModel
	for _, m := range running {
		used += m.GPU
		if !m.Stays && m.GPU > 0 {
			free = append(free, m)
		}
	}
	if used+want <= vram {
		return nil
	}
	sort.SliceStable(free, func(i, j int) bool { return free[i].LastUsed < free[j].LastUsed })
	var stop []gpuModel
	for _, m := range free {
		if used+want <= vram {
			break
		}
		stop = append(stop, m)
		used -= m.GPU
	}
	return stop
}

// dedicatedVRAM is the graphics memory of this computer's largest card
// with memory of its own, or 0 when it has none.
func (a *App) dedicatedVRAM(ctx context.Context) uint64 {
	hw, err := a.detectHardware(ctx)
	if err != nil {
		return 0
	}
	most := uint64(0)
	for _, ac := range hw.Accelerators {
		if ac.Kind == "gpu" && !softwareAdapter(ac.Model) {
			most = max(most, ac.DedicatedVRAM)
		}
	}
	return most
}

// freeGPUFor unloads the idle models holding the graphics card so modelID
// starts on it rather than on the CPU.
func (a *App) freeGPUFor(ctx context.Context, modelID string) {
	if a.Runtimes == nil || a.stubInference || a.Models == nil || a.hw == nil {
		return
	}
	views, err := a.localRunningViews(ctx)
	if err != nil || len(views) == 0 {
		return
	}
	for _, v := range views {
		if v.ModelID == modelID {
			return
		}
	}
	m, err := a.Models.Get(ctx, modelID)
	if err != nil || m.MemoryNeeded == 0 {
		return
	}
	vram := a.dedicatedVRAM(ctx)
	if vram == 0 {
		return
	}
	pinned := a.alwaysRunningList(ctx)
	var generating map[string]bool
	if a.Health != nil {
		generating = a.Health.Generating()
	}
	running := make([]gpuModel, 0, len(views))
	for _, v := range views {
		g := gpuModel{InstanceID: v.InstanceID, ModelID: v.ModelID}
		if v.Acceleration != nil {
			g.GPU = v.Acceleration.GPUMemoryBytes
		}
		if v.LastUsedAt != nil {
			g.LastUsed = v.LastUsedAt.UnixNano()
		}
		g.Stays = slices.Contains(pinned, v.ModelID) || generating[v.ModelID] || a.loadingNow(v.ModelID)
		running = append(running, g)
	}
	for _, s := range gpuUnloads(running, m.MemoryNeeded, vram) {
		if err := a.stopModelLocal(ctx, s.InstanceID); err != nil {
			if a.Logger != nil {
				a.Logger.Warn("unload for room on the graphics card", "model_id", s.ModelID, "for", modelID, "error", err)
			}
			continue
		}
		if a.Logger != nil {
			a.Logger.Info("unloaded a model for room on the graphics card", "model", s.ModelID, "for", modelID, "gpu_bytes", s.GPU)
		}
		if a.Bus != nil {
			a.Bus.Publish(events.New(events.ModelUnloaded, map[string]any{
				"model_id": s.ModelID, "instance_id": s.InstanceID, "reason": "gpu_room",
			}))
		}
	}
}
