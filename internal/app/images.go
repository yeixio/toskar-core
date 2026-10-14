package app

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/imagegen"
	"github.com/yeixio/toskar-core/internal/runtimes/llamacpp"
)

// newImageSetup keeps stable-diffusion.cpp with the runtimes and image
// models with the models, and sizes its recommendation to this computer.
func (a *App) newImageSetup(cfg config.Config) *imagegen.Setup {
	return a.newSDSetup(cfg, "images", nil)
}

// newVideoSetup is video generation's setup: the same stable-diffusion.cpp,
// with video models in their own folder (Gungnir §27).
func (a *App) newVideoSetup(cfg config.Config) *imagegen.Setup {
	return a.newSDSetup(cfg, "video", imagegen.VideoCatalog)
}

func (a *App) newSDSetup(cfg config.Config, folder string, catalog func() []imagegen.Model) *imagegen.Setup {
	memory := sync.OnceValue(func() int64 {
		inv, err := a.hw.Detect(context.Background())
		if err != nil {
			return 0
		}
		return int64(inv.Memory.TotalBytes)
	})
	// The memory graphics can use: a card's own, or the share of system
	// memory Apple silicon gives its graphics; 0 without either (#550).
	graphics := sync.OnceValue(func() int64 {
		inv, err := a.hw.Detect(context.Background())
		if err != nil {
			return 0
		}
		var most uint64
		for _, ac := range inv.Accelerators {
			if ac.Kind != "gpu" || softwareAdapter(ac.Model) {
				continue
			}
			if ac.DedicatedVRAM == 0 && ac.UnifiedMemory > 0 {
				most = max(most, sharedGraphics(ac.UnifiedMemory))
			} else {
				most = max(most, ac.DedicatedVRAM)
			}
		}
		return int64(most)
	})
	shared := sync.OnceValue(func() bool {
		inv, err := a.hw.Detect(context.Background())
		if err != nil {
			return false
		}
		for _, ac := range inv.Accelerators {
			if ac.Kind == "gpu" && ac.DedicatedVRAM == 0 && ac.UnifiedMemory > 0 {
				return true
			}
		}
		return false
	})
	return &imagegen.Setup{
		ProgramDir: filepath.Join(cfg.RuntimesDir, "sdcpp"),
		Graphics:   graphics,
		Shared:     shared,
		ModelsDir:  filepath.Join(cfg.ModelsDir, folder),
		Catalog:    catalog,
		Memory:     memory,
		FreeBytes: func() (int64, error) {
			inv, err := a.hw.Detect(context.Background())
			if err != nil {
				return 0, err
			}
			return int64(inv.Disk.AvailableBytes), nil
		},
		Sandboxed: a.python.Sandboxed,
		// Models behind a license download with the person's own token.
		Token: a.huggingFaceToken,
		// The Vulkan build when a GPU can run it (#154), checked once: it
		// runs vulkaninfo, and the answer doesn't change while running.
		GPU: vulkanUsable,
		// "Can you make images?" and setup offers read the inventory.
		Changed: a.invalidateCapabilities,
	}
}

// vulkanUsable reports, once, whether a Vulkan build would use a GPU here;
// images, video, and llama.cpp agree on it.
var vulkanUsable = sync.OnceValue(func() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return llamacpp.VulkanUsable(ctx)
})

// sharedGraphics is how much of Apple silicon's memory its graphics may
// use: macOS lets the GPU wire about two thirds of it, and three quarters
// above 36 GB (its recommendedMaxWorkingSetSize). 24 GB of memory isn't
// 24 GB of graphics memory (#550).
func sharedGraphics(memory uint64) uint64 {
	if memory > 36<<30 {
		return memory / 4 * 3
	}
	return memory / 3 * 2
}
