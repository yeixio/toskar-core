package models

import "github.com/yeixio/toskar-core/pkg/contracts"

// autoContextSizes are the windows Auto picks between (#962), smallest
// first. llama-server holds a window's whole cache for as long as the
// model runs, a few gigabytes past 32K, so a bigger one is for a model
// set to it or a request that needs it, not the default.
var autoContextSizes = []int{runningContextTokens, 16384, 32768}

// AutoContext is the window a chat model runs with on hw when nothing else
// is chosen (#962): the largest of autoContextSizes, up to the model's own
// window, at which it's still an excellent fit and its high estimate fits
// the graphics memory it runs in. A model that's only a good fit at 8K
// stays there, as before; one whose own window is smaller runs at that; one
// whose window isn't known gets 8K.
func AutoContext(e CatalogEntry, hw contracts.HardwareInventory) int {
	own := e.Context
	if own > 0 && own < runningContextTokens {
		return own
	}
	window := runningContextTokens
	if own <= 0 {
		return window
	}
	for _, n := range autoContextSizes[1:] {
		if n > own {
			break
		}
		fit := ClassifyFit(e, hw, "", "", FitOptions{ContextTokens: n})
		if fit.Label != contracts.FitExcellent || !graphicsRoom(fit, hw) {
			break
		}
		window = n
	}
	return window
}

// graphicsRoom reports whether fit's high estimate stays in the graphics
// memory the model runs in: a graphics card's own, or the share of a
// Mac's memory its graphics may use, less what makes a fit tight there.
// A bigger window that spills layers to the processor isn't worth it.
func graphicsRoom(fit contracts.ModelFit, hw contracts.HardwareInventory) bool {
	if fit.MemoryKind == "unified" {
		return float64(fit.RuntimeMemoryHighBytes) <= float64(SharedGraphics(fit.TotalMemoryBytes))*graphicsTightShare
	}
	if card := cardMemory(hw); card > 0 {
		return fit.RuntimeMemoryHighBytes <= card
	}
	return true
}
