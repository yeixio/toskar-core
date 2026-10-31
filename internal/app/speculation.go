package app

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// speculationOffKey is the setting that lists the models with speculative
// decoding turned off (#835). Every other chat model uses Auto: on where
// this llama-server has it.
const speculationOffKey = "speculation_off"

// Speculation choices for a model (contract 1.67).
const (
	SpeculationAuto = "auto"
	SpeculationOff  = "off"
)

// speculationOff reports whether the person turned speculative decoding off
// for modelID.
func (a *App) speculationOff(ctx context.Context, modelID string) bool {
	return slices.Contains(a.speculationOffList(ctx), modelID)
}

func (a *App) speculationOffList(ctx context.Context) []string {
	if a.Settings == nil {
		return nil
	}
	raw, ok, err := a.Settings.Get(ctx, speculationOffKey)
	if err != nil || !ok || raw == "" {
		return nil
	}
	var ids []string
	if json.Unmarshal([]byte(raw), &ids) != nil {
		return nil
	}
	return ids
}

// setModelSpeculation sets Auto or Off for a chat model installed on this
// computer. A loaded model keeps how it started until it loads again.
func (a *App) setModelSpeculation(ctx context.Context, modelID, choice string) error {
	if choice != SpeculationAuto && choice != SpeculationOff {
		return fmt.Errorf("speculation must be %q or %q", SpeculationAuto, SpeculationOff)
	}
	m, err := a.Models.Get(ctx, modelID)
	if err != nil || !m.Installed {
		return fmt.Errorf("model %s isn't installed on this computer", modelID)
	}
	if contracts.SupportRoleOf(m) != "" {
		return fmt.Errorf("model %s doesn't chat, so it doesn't guess ahead", modelID)
	}
	ids := slices.DeleteFunc(a.speculationOffList(ctx), func(id string) bool { return id == modelID })
	if choice == SpeculationOff {
		ids = append(ids, modelID)
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return a.Settings.Set(ctx, speculationOffKey, string(raw))
}

// withSpeculation says Auto or Off on each chat model installed on this
// computer.
func (a *App) withSpeculation(ctx context.Context, list []contracts.Model) {
	off := a.speculationOffList(ctx)
	for i, m := range list {
		if !m.Installed || contracts.SupportRoleOf(m) != "" {
			continue
		}
		list[i].Speculation = SpeculationAuto
		if slices.Contains(off, m.ID) {
			list[i].Speculation = SpeculationOff
		}
	}
}

// draftPath is the draft a model starts with: its draft when the model,
// draft and all, fits this computer well. On one where it fits only tightly,
// such as a graphics card too small for all of it, the draft would take
// memory the model needs, so it guesses from the chat alone.
func (a *App) draftPath(ctx context.Context, modelID string) string {
	path := a.Models.DraftPath(modelID)
	if path == "" {
		return ""
	}
	hw, err := a.detectHardware(ctx)
	if err != nil {
		return ""
	}
	cfg := a.Config.Get()
	running, _ := a.listRunningAll(ctx)
	for _, f := range a.Models.Fit(hw, cfg.NodeID, cfg.NodeName, a.fitOptions(ctx, cfg.NodeID, true, running)).Fits {
		if f.ModelID == modelID {
			if f.Label == contracts.FitExcellent || f.Label == contracts.FitGood {
				return path
			}
			return ""
		}
	}
	return ""
}
