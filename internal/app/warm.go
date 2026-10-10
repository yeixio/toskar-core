package app

import (
	"context"
	"time"

	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// warmFor bounds a warm-up's load, which runs after its request has ended.
const warmFor = 5 * time.Minute

// warmRoom is how much of this computer's memory loaded models may use
// before a warm-up leaves the next one for the question itself to load.
const warmRoom = 0.85

// warmChat starts loading the model a chat would use, without waiting for
// it (#498). A question from another device after a quiet stretch otherwise
// waits for the model to load; a warm-up sent when someone starts asking
// lets the load run while they type or speak. It never disturbs a model
// that is answering, and never loads one that wouldn't fit beside those
// already loaded.
func (a *App) warmChat(ctx context.Context, req contracts.ModelWarmRequest) (contracts.ModelWarmResponse, error) {
	modelID, err := a.warmModelID(ctx, req)
	if err != nil {
		return contracts.ModelWarmResponse{}, err
	}
	if modelID == "" {
		return contracts.ModelWarmResponse{Status: "none"}, nil
	}
	running, _ := a.localRunningViews(ctx)
	_, inflight := a.warming.Load(modelID)
	generating := a.Health != nil && len(a.Health.Generating()) > 0
	status := warmStatus(modelID, a.installedModels(ctx), running, inflight, generating, a.memoryTotal(ctx))
	if status != "load" {
		return contracts.ModelWarmResponse{ModelID: modelID, Status: status}, nil
	}
	if _, loading := a.warming.LoadOrStore(modelID, struct{}{}); loading {
		return contracts.ModelWarmResponse{ModelID: modelID, Status: "loading"}, nil
	}
	a.warmWait.Add(1)
	go func() {
		defer a.warmWait.Done()
		defer a.warming.Delete(modelID)
		load, cancel := context.WithTimeout(context.WithoutCancel(ctx), warmFor)
		defer cancel()
		if _, err := a.startModelLocal(load, modelID); err != nil && a.Logger != nil {
			a.Logger.Warn("warm-up load failed", "model", modelID, "error", err)
		}
	}()
	return contracts.ModelWarmResponse{ModelID: modelID, Status: "loading"}, nil
}

// warmModelID is the model a chat on Auto would use for a general question,
// as a turn picks it: the profile's fast model, or else Auto's choice.
func (a *App) warmModelID(ctx context.Context, req contracts.ModelWarmRequest) (string, error) {
	if req.ModelID != "" && req.ModelID != huginn.AutoModelID {
		return req.ModelID, nil
	}
	profileID := req.ProfileID
	if profileID == "" {
		profileID = a.defaultProfileID(ctx)
	}
	if profileID != "" && a.Profiles != nil {
		if profile, err := a.Profiles.Get(ctx, profileID); err == nil {
			if id, _, ok := a.profileRoleModel(ctx, a.appLanguage(ctx), profile, "", false); ok {
				return id, nil
			}
		}
	}
	choice, err := a.chooseAuto(ctx, "", false, "")
	if err != nil {
		if err == errNoModel {
			return "", nil
		}
		return "", err
	}
	return choice.Model.ID, nil
}

// warmStatus decides a warm-up: "load" to start loading modelID now, or the
// status that answers the request instead. running are the models loaded on
// this computer, with the memory each uses.
func warmStatus(modelID string, installed []contracts.Model, running []contracts.RunningModelView, inflight, generating bool, memTotal uint64) string {
	need, here := uint64(0), false
	for _, m := range installed {
		if m.ID == modelID && m.Installed {
			need, here = m.MemoryNeeded, true
		}
	}
	if !here {
		return "not_local"
	}
	used := uint64(0)
	for _, v := range running {
		if v.ModelID == modelID {
			return "loaded"
		}
		used += v.MemoryBytes
	}
	if inflight {
		return "loading"
	}
	// A model answering now keeps the computer's attention; a load beside it
	// would slow that answer down.
	if generating {
		return "busy"
	}
	if memTotal > 0 && need > 0 && float64(used+need) > float64(memTotal)*warmRoom {
		return "no_room"
	}
	return "load"
}
