package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yeixio/yggdrasil-core/internal/artifacts"
	"github.com/yeixio/yggdrasil-core/internal/huginn"
	modelhealth "github.com/yeixio/yggdrasil-core/internal/models/health"
	"github.com/yeixio/yggdrasil-core/internal/profiles"
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
func (a *App) chooseAuto(ctx context.Context, message string, data bool) (huginn.Choice, error) {
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
	kind := huginn.Classify(message)
	if data && kind == huginn.Chat {
		kind = huginn.Research
	}
	choice, ok := huginn.ChooseFor(kind, huginn.EffortFrom(ctx), usable, a.memoryTotal(ctx))
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
// preferred is the profile's fallback order, tried before Yggdrasil's pick.
func (a *App) fallback(ctx context.Context, failedID, errText string, preferred []string) (model contracts.Model, step, notice string, ok bool) {
	return fallbackFrom(failedID, errText, a.installedModels(ctx), a.memoryTotal(ctx), preferred...)
}

func fallbackFrom(failedID, errText string, installed []contracts.Model, memTotal uint64, preferred ...string) (model contracts.Model, step, notice string, ok bool) {
	next, ok := preferredFallback(failedID, installed, preferred)
	if !ok {
		next, ok = huginn.Fallback(failedID, installed, memTotal)
	}
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

// preferredFallback is the first model in a profile's fallback order that is
// installed, can answer chats, and is not the one that failed (§20).
func preferredFallback(failedID string, installed []contracts.Model, preferred []string) (contracts.Model, bool) {
	for _, id := range preferred {
		if id == failedID {
			continue
		}
		for _, m := range installed {
			if m.ID == id && (m.Installed || m.Status == "installed") && !huginn.Supporting(m) {
				return m, true
			}
		}
	}
	return contracts.Model{}, false
}

// profileRoleModel is the profile's own model for this kind of request when
// the chat is on Auto (§20): its coding model for coding, and its fast model
// for a quick question. ok is false when the profile has none installed.
func (a *App) profileRoleModel(ctx context.Context, profile profiles.Profile, message string, data bool) (id, reason string, ok bool) {
	kind := huginn.Classify(message)
	if data && kind == huginn.Chat {
		kind = huginn.Research
	}
	role := ""
	switch kind {
	case huginn.Coding:
		role = profiles.RoleCoding
	case huginn.Chat:
		role = profiles.RoleFast
	default:
		return "", "", false
	}
	id = profiles.RoleModel(profile, role)
	if id == "" || a.recentlyFailed(id) {
		return "", "", false
	}
	for _, m := range a.installedModels(ctx) {
		if m.ID == id && (m.Installed || m.Status == "installed") && !huginn.Supporting(m) {
			what := "coding model"
			if role == profiles.RoleFast {
				what = "model for quick questions"
			}
			return id, fmt.Sprintf("Used %s, the %s of %s", huginn.Name(m), what, profile.Name), true
		}
	}
	return "", "", false
}

// turnHasData reports whether a turn will answer from the user's own data:
// files attached now or earlier in the chat, or connected knowledge.
func (a *App) turnHasData(ctx context.Context, conversationID string, profile profiles.Profile) bool {
	if len(artifacts.AttachmentsFrom(ctx)) > 0 || len(profile.KnowledgeSources) > 0 {
		return true
	}
	if a.Artifacts != nil && conversationID != "" {
		if list, err := a.Artifacts.List(ctx, conversationID); err == nil && len(list) > 0 {
			return true
		}
	}
	return false
}

// smallModelNotice warns that a small model answered from the user's files
// or knowledge, where it is most likely to mix up details, and suggests a
// larger model. dataKind is "file", "knowledge", or "" when no data was used.
func (a *App) smallModelNotice(ctx context.Context, dataKind, modelID string, auto bool) string {
	if dataKind == "" || modelID == "" {
		return ""
	}
	return smallModelNote(dataKind, modelID, a.installedModels(ctx), a.memoryTotal(ctx), auto)
}

func smallModelNote(dataKind, modelID string, installed []contracts.Model, memTotal uint64, auto bool) string {
	if dataKind == "" || modelID == "" {
		return ""
	}
	var answered contracts.Model
	for _, m := range installed {
		if m.ID == modelID {
			answered = m
		}
	}
	if !huginn.Small(answered) {
		return ""
	}
	from := "your files"
	if dataKind == "knowledge" {
		from = "your knowledge"
	}
	note := fmt.Sprintf("%s is a small model and can mix up numbers and details from %s. Check them against the source.", huginn.Name(answered), from)
	if larger, ok := huginn.Larger(installed, memTotal); ok && !auto {
		note += fmt.Sprintf(" For questions like this, choose %s or Auto in Model.", huginn.Name(larger))
	} else if !ok {
		note += " A larger model from the Models page will be more reliable."
	}
	return note
}
