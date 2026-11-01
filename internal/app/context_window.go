package app

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

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
	base := a.baseWindow(ctx, modelID, entry)
	if r, ok := a.raisedWindows.Load(modelID); ok {
		raised := r.(raisedWindow)
		if raised.tokens > base && (time.Since(raised.at) < raiseGrace || a.runningWith(ctx, modelID, raised.tokens)) {
			return raised.tokens
		}
		a.raisedWindows.Delete(modelID)
	}
	return base
}

// baseWindow is the window a model starts with when no request has asked
// for more: the person's choice, else Auto.
func (a *App) baseWindow(ctx context.Context, modelID string, entry models.CatalogEntry) int {
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

// raisedWindow is a bigger window a request asked a model for (#962), and
// when.
type raisedWindow struct {
	tokens int
	at     time.Time
}

// raiseGrace is how long a raised window holds before the model has started
// again with it. After that it holds while the model runs with it, and once
// the model stops, it starts with its own window again.
const raiseGrace = 2 * time.Minute

// growWindow raises a model installed here to the smallest size that holds
// need tokens, up to its own window, at which it still fits this computer,
// tightly at worst: the request asked for it, so it's worth the memory
// (#962). The model starts again with it on its next request. It returns
// the new window, or 0 when no size fits.
func (a *App) growWindow(ctx context.Context, modelID string, need int) int {
	if a.Models == nil || need <= 0 {
		return 0
	}
	entry, ok := a.Models.Catalog().Get(modelID)
	if !ok {
		return 0
	}
	if _, err := a.Models.Path(ctx, modelID); err != nil {
		return 0
	}
	hw, ok := a.steadyHardware(ctx)
	if !ok {
		return 0
	}
	current := a.startWindow(ctx, modelID)
	if current >= need {
		return 0
	}
	for _, n := range contextSizes {
		if n < need {
			continue
		}
		if n > ownWindow(entry) {
			break
		}
		switch models.ClassifyFit(entry, hw, "", "", models.FitOptions{ContextTokens: n}).Label {
		case contracts.FitExcellent, contracts.FitGood, contracts.FitTight:
			a.raisedWindows.Store(modelID, raisedWindow{tokens: n, at: time.Now()})
			return n
		}
		return 0
	}
	return 0
}

// runningWith reports whether modelID runs here with a window of at least
// tokens.
func (a *App) runningWith(ctx context.Context, modelID string, tokens int) bool {
	if a.Runtimes == nil {
		return false
	}
	list, err := a.Runtimes.ListRunning(ctx, "llamacpp")
	if err != nil {
		return false
	}
	for _, m := range list {
		if m.ModelID == modelID && m.Status == "running" && m.Context >= tokens {
			return true
		}
	}
	return false
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
