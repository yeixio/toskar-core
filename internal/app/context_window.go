package app

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/yeixio/toskar-core/internal/models"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// contextWindowsKey is the setting that keeps the context window the
// person chose for each model on this computer (#962): model id -> tokens.
// A model that isn't listed uses Auto.
const contextWindowsKey = "context_windows"

// contextSizes are the windows a person can choose, up to the model's own.
var contextSizes = []int{4096, 8192, 16384, 32768, 65536, 131072, 262144}

// unknownWindow is the most a model whose own window isn't known may use:
// past what it was made for, a model's answers fall apart.
const unknownWindow = 8192

// startWindow is the context window a chat model starts with on this
// computer (#962): the size the person chose for it, else Auto, the largest
// that fits its memory well. 0 leaves the runtime's default, for a model the
// catalog doesn't know or hardware that can't be read.
func (a *App) startWindow(ctx context.Context, modelID string) int {
	if a.Models == nil {
		return 0
	}
	entry, ok := a.Models.Catalog().Get(modelID)
	if !ok {
		return 0
	}
	if n := a.contextChoices(ctx)[modelID]; n > 0 {
		return min(n, ownWindow(entry))
	}
	if v, ok := a.startWindows.Load(modelID); ok {
		return v.(int)
	}
	hw, ok := a.steadyHardware(ctx)
	if !ok {
		return 0
	}
	n := models.AutoContext(entry, hw)
	a.startWindows.Store(modelID, n)
	return n
}

// ownWindow is the most a model may use: its own window, or unknownWindow.
func ownWindow(e models.CatalogEntry) int {
	if e.Context > 0 {
		return e.Context
	}
	return unknownWindow
}

// steadyHardware is this computer's hardware, detected once: memory and
// graphics cards don't change while Toskar runs, and detecting them runs
// other programs. Free memory in it goes stale, so it's only for what
// AutoContext and the fit labels use.
func (a *App) steadyHardware(ctx context.Context) (contracts.HardwareInventory, bool) {
	a.steadyHW.Lock()
	defer a.steadyHW.Unlock()
	if a.steadyHW.known {
		return a.steadyHW.inv, true
	}
	if a.hw == nil {
		return contracts.HardwareInventory{}, false
	}
	hw, err := a.detectHardware(ctx)
	if err != nil || hw.Memory.TotalBytes == 0 {
		return contracts.HardwareInventory{}, false
	}
	a.steadyHW.inv, a.steadyHW.known = hw, true
	return hw, true
}

func (a *App) contextChoices(ctx context.Context) map[string]int {
	if a.Settings == nil {
		return nil
	}
	raw, ok, err := a.Settings.Get(ctx, contextWindowsKey)
	if err != nil || !ok || raw == "" {
		return nil
	}
	var m map[string]int
	if json.Unmarshal([]byte(raw), &m) != nil {
		return nil
	}
	return m
}

// setModelContext sets the context window for a chat model installed on
// this computer: one of contextSizes up to its own window, or 0 for Auto. A
// loaded model starts again with it on its next request.
func (a *App) setModelContext(ctx context.Context, modelID string, tokens int) error {
	m, err := a.Models.Get(ctx, modelID)
	if err != nil || !m.Installed {
		return fmt.Errorf("model %s isn't installed on this computer", modelID)
	}
	if contracts.SupportRoleOf(m) != "" {
		return fmt.Errorf("model %s doesn't chat, so it has no context window to set", modelID)
	}
	entry, _ := a.Models.Catalog().Get(modelID)
	if tokens != 0 && (!slices.Contains(contextSizes, tokens) || tokens > ownWindow(entry)) {
		return fmt.Errorf("the context window for %s is 0 for Auto, or one of %v up to %d", modelID, contextSizes, ownWindow(entry))
	}
	choices := a.contextChoices(ctx)
	if choices == nil {
		choices = map[string]int{}
	}
	if tokens == 0 {
		delete(choices, modelID)
	} else {
		choices[modelID] = tokens
	}
	raw, err := json.Marshal(choices)
	if err != nil {
		return err
	}
	return a.Settings.Set(ctx, contextWindowsKey, string(raw))
}

// withContextWindows says, on each chat model installed on this computer,
// the window it starts with, the size the person chose (0 for Auto), and
// each size it can take with how well the model fits at it (#962).
func (a *App) withContextWindows(ctx context.Context, list []contracts.Model) {
	hw, ok := a.steadyHardware(ctx)
	if !ok || a.Models == nil {
		return
	}
	choices := a.contextChoices(ctx)
	for i, m := range list {
		if !m.Installed || contracts.SupportRoleOf(m) != "" {
			continue
		}
		entry, ok := a.Models.Catalog().Get(m.ID)
		if !ok {
			continue
		}
		list[i].ContextWindow = a.startWindow(ctx, m.ID)
		list[i].ContextChoice = choices[m.ID]
		for _, n := range contextSizes {
			if n > ownWindow(entry) {
				break
			}
			fit := models.ClassifyFit(entry, hw, "", "", models.FitOptions{ContextTokens: n})
			list[i].ContextOptions = append(list[i].ContextOptions, contracts.ContextOption{Tokens: n, Fit: fit.Label})
		}
	}
}
