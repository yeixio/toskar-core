package app

import (
	"context"

	"github.com/yeixio/toskar-core/internal/models"
)

// startWindow is the context window a chat model starts with on this
// computer (#962): Auto picks the largest that fits its memory well. It's
// worked out once per model, since detecting the hardware runs other
// programs. 0 leaves the runtime's default, for a model the catalog doesn't
// know or hardware that can't be read.
func (a *App) startWindow(ctx context.Context, modelID string) int {
	if v, ok := a.startWindows.Load(modelID); ok {
		return v.(int)
	}
	if a.Models == nil || a.hw == nil {
		return 0
	}
	entry, ok := a.Models.Catalog().Get(modelID)
	if !ok {
		return 0
	}
	hw, err := a.detectHardware(ctx)
	if err != nil || hw.Memory.TotalBytes == 0 {
		return 0
	}
	n := models.AutoContext(entry, hw)
	a.startWindows.Store(modelID, n)
	return n
}
