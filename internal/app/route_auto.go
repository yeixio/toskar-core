package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yeixio/yggdrasil-core/internal/huginn"
	modelhealth "github.com/yeixio/yggdrasil-core/internal/models/health"
	"github.com/yeixio/yggdrasil-core/pkg/contracts"
)

// errNoModel is returned when Auto has nothing to choose from.
var errNoModel = errors.New("no model is installed yet. Install one on the Models page")

// memoryTotal is this computer's memory in bytes, detected once. It is 0 when
// it cannot be read, which lets every model through.
func (a *App) memoryTotal(ctx context.Context) uint64 {
	if v := a.memTotal.Load(); v > 0 {
		return v
	}
	if a.hw == nil {
		return 0
	}
	inv, err := a.hw.Detect(ctx)
	if err != nil {
		return 0
	}
	a.memTotal.Store(inv.Memory.TotalBytes)
	return inv.Memory.TotalBytes
}

func (a *App) installedModels(ctx context.Context) []contracts.Model {
	if a.Models == nil {
		return nil
	}
	list, err := a.Models.List(ctx)
	if err != nil {
		return nil
	}
	return list
}

// failedFor is how long Auto avoids a model that could not answer.
const failedFor = 10 * time.Minute

// noteModelFailed records that a model could not answer, so Auto passes it
// over for a while instead of failing and falling back on every turn.
func (a *App) noteModelFailed(modelID string) {
	a.failedModels.Store(modelID, time.Now())
}

func (a *App) recentlyFailed(modelID string) bool {
	v, ok := a.failedModels.Load(modelID)
	if !ok {
		return false
	}
	if time.Since(v.(time.Time)) > failedFor {
		a.failedModels.Delete(modelID)
		return false
	}
	return true
}

// chooseAuto picks the model for a chat set to Auto (spec §12–13).
func (a *App) chooseAuto(ctx context.Context, message string) (huginn.Choice, error) {
	all := a.installedModels(ctx)
	usable := make([]contracts.Model, 0, len(all))
	for _, m := range all {
		if !a.recentlyFailed(m.ID) {
			usable = append(usable, m)
		}
	}
	if len(usable) == 0 {
		usable = all
	}
	choice, ok := huginn.Choose(huginn.Classify(message), usable, a.memoryTotal(ctx))
	if !ok {
		return huginn.Choice{}, errNoModel
	}
	return choice, nil
}

// recoverable reports whether a failed turn may be retried on another model:
// nothing was shown or changed yet, and the user did not stop it.
func recoverable(ctx context.Context, errText, shown string, env *chatExecEnv) bool {
	if ctx.Err() != nil || shown != "" || env.trace.hasSideEffects() {
		return false
	}
	lower := strings.ToLower(errText)
	return !strings.Contains(lower, "context canceled") && !strings.Contains(lower, "cancelled")
}

// fallback picks the model to retry with and the words that explain it
// (spec §14, §26). The note is empty when the answer should be as good.
func (a *App) fallback(ctx context.Context, failedID, errText string) (model contracts.Model, step, notice string, ok bool) {
	return fallbackFrom(failedID, errText, a.installedModels(ctx), a.memoryTotal(ctx))
}

func fallbackFrom(failedID, errText string, installed []contracts.Model, memTotal uint64) (model contracts.Model, step, notice string, ok bool) {
	next, ok := huginn.Fallback(failedID, installed, memTotal)
	if !ok {
		return contracts.Model{}, "", "", false
	}
	failed := contracts.Model{ID: failedID}
	for _, m := range installed {
		if m.ID == failedID {
			failed = m
		}
	}
	why := "could not answer"
	if f, isHealth := modelhealth.Parse(errText); isHealth {
		why = "stopped responding"
		if f.LikelyMemoryPressure {
			why = "ran out of memory"
		}
	}
	step = fmt.Sprintf("%s %s, so %s answered instead", huginn.Name(failed), why, huginn.Name(next))
	if huginn.Smaller(failed, next) {
		notice = fmt.Sprintf("%s could not answer, so the smaller %s answered instead. This answer may be less detailed.", huginn.Name(failed), huginn.Name(next))
	}
	return next, step, notice, true
}
