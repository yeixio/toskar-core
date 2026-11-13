package app

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/yeixio/toskar-core/internal/contextusage"
	"github.com/yeixio/toskar-core/internal/models"
	"github.com/yeixio/toskar-core/internal/runtimes/llamacpp"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// modelSlotsKey is the setting that keeps how many requests each chat model
// on this computer answers at once (#1135): model id -> slots. A model that
// isn't listed answers one at a time.
const modelSlotsKey = "model_slots"

// slotWindowFloor is the least window each slot may have: the window every
// chat and task counts on when nothing bigger is known.
const slotWindowFloor = contextusage.DefaultWindow

func (a *App) slotChoices(ctx context.Context) map[string]int {
	if a.Settings == nil {
		return nil
	}
	raw, ok, err := a.Settings.Get(ctx, modelSlotsKey)
	if err != nil || !ok || raw == "" {
		return nil
	}
	var m map[string]int
	if json.Unmarshal([]byte(raw), &m) != nil {
		return nil
	}
	return m
}

// modelSlots is how many requests modelID answers at once on this computer:
// the person's setting, from 1 to llamacpp.MaxSlots.
func (a *App) modelSlots(ctx context.Context, modelID string) int {
	return min(max(1, a.slotChoices(ctx)[modelID]), llamacpp.MaxSlots)
}

// slotFit is the fit options for modelID at a window of tokens per slot,
// with its slots.
func (a *App) slotFit(ctx context.Context, modelID string, tokens int) models.FitOptions {
	opts := models.FitOptions{ContextTokens: tokens}
	if n := a.modelSlots(ctx, modelID); n > 1 {
		opts.Slots = map[string]int{modelID: n}
	}
	return opts
}

// setModelSlots sets how many requests a chat model installed on this
// computer answers at once, so a swarm can run that many agents on it
// together (#1135). Each slot keeps the model's whole window, so the cache
// grows with them. It refuses a number that leaves a slot less than
// slotWindowFloor, or that doesn't fit this computer. A loaded model starts
// again with it on its next request.
func (a *App) setModelSlots(ctx context.Context, modelID string, slots int) error {
	if slots < 1 || slots > llamacpp.MaxSlots {
		return contracts.Errorf("MODEL_SLOTS_INVALID", map[string]any{"max": llamacpp.MaxSlots},
			"slots must be 1 to %d", llamacpp.MaxSlots)
	}
	m, err := a.Models.Get(ctx, modelID)
	if err != nil || !m.Installed {
		return fmt.Errorf("model %s isn't installed on this computer", modelID)
	}
	if contracts.SupportRoleOf(m) != "" {
		return fmt.Errorf("model %s doesn't chat, so it answers one request at a time", modelID)
	}
	if slots > 1 {
		if err := a.checkSlots(ctx, modelID, slots); err != nil {
			return err
		}
	}
	choices := a.slotChoices(ctx)
	if choices == nil {
		choices = map[string]int{}
	}
	if slots == 1 {
		delete(choices, modelID)
	} else {
		choices[modelID] = slots
	}
	raw, err := json.Marshal(choices)
	if err != nil {
		return err
	}
	return a.Settings.Set(ctx, modelSlotsKey, string(raw))
}

// checkSlots says whether modelID can answer slots requests at once here:
// each slot needs at least slotWindowFloor, and every slot's cache must fit,
// tightly at worst, as a raised window must.
func (a *App) checkSlots(ctx context.Context, modelID string, slots int) error {
	entry, known := a.Models.Catalog().Get(modelID)
	perSlot := 0
	if known {
		perSlot = a.windowWithSlots(ctx, modelID, entry, slots)
	}
	if perSlot <= 0 {
		// The runtime's default, which a model the catalog doesn't know
		// starts with.
		perSlot = slotWindowFloor
	}
	if perSlot < slotWindowFloor {
		return contracts.Errorf("MODEL_SLOTS_CONTEXT",
			map[string]any{"slots": slots, "context_per_slot": perSlot, "needed": slotWindowFloor},
			"with %d slots, each would have a context of %d tokens, less than the %d a task needs; choose a bigger context window or fewer slots",
			slots, perSlot, slotWindowFloor)
	}
	hw, ok := a.steadyHardware(ctx)
	if !known || !ok {
		return nil
	}
	fit := models.ClassifyFit(entry, hw, "", "", models.FitOptions{ContextTokens: perSlot, Slots: map[string]int{modelID: slots}})
	switch fit.Label {
	case contracts.FitExcellent, contracts.FitGood, contracts.FitTight:
		return nil
	}
	return contracts.Errorf("MODEL_SLOTS_MEMORY", map[string]any{"slots": slots},
		"%s doesn't fit this computer's memory with %d slots of %d tokens each", modelID, slots, perSlot)
}

// withSlots says, on each chat model installed on this computer, how many
// requests it answers at once.
func (a *App) withSlots(ctx context.Context, list []contracts.Model) {
	choices := a.slotChoices(ctx)
	for i, m := range list {
		if !m.Installed || contracts.SupportRoleOf(m) != "" {
			continue
		}
		list[i].Slots = min(max(1, choices[m.ID]), llamacpp.MaxSlots)
	}
}

// listModelsForPeers is this computer's models for a paired computer, with
// each chat model's slots, so its swarms know how many agents fit here.
func (a *App) listModelsForPeers(ctx context.Context) ([]contracts.Model, error) {
	list, err := a.Models.List(ctx)
	if err != nil {
		return nil, err
	}
	a.withSlots(ctx, list)
	return list, nil
}

// remoteSlotKey is a paired computer's model.
type remoteSlotKey struct{ nodeID, modelID string }

// noteRemoteSlots keeps the slots a paired computer said its models have.
// A computer from before slots says none, and its models take one request
// at a time.
func (a *App) noteRemoteSlots(nodeID string, list []contracts.Model) {
	for _, m := range list {
		if m.Installed {
			a.remoteSlots.Store(remoteSlotKey{nodeID, m.ID}, max(1, m.Slots))
		}
	}
}

// swarmSlots is how many of a swarm's agents a model on a computer runs at
// once (swarm.Group): this computer's setting, or what a paired computer
// last said when placement asked it for its models; 1 when unknown.
func (a *App) swarmSlots(nodeID, modelID string) int {
	if a.SwarmSlots != nil {
		return a.SwarmSlots(nodeID, modelID)
	}
	if nodeID == "" || nodeID == a.Config.Get().NodeID {
		return a.modelSlots(context.Background(), modelID)
	}
	if v, ok := a.remoteSlots.Load(remoteSlotKey{nodeID, modelID}); ok {
		return v.(int)
	}
	return 1
}
