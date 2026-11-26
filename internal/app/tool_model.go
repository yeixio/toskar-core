package app

import (
	"context"
	"slices"
)

// toolModel is the model on this computer a request with the client's own
// tools goes to (#1450), such as Home Assistant's Assist or n8n's AI Agent
// asking for auto: they run their tools themselves, so the model answers
// with tool_calls as raw:<model> does. It's model when that's one installed
// here, else the profile's assistant model, the default profile's for none
// or auto, else the model used last.
func (a *App) toolModel(ctx context.Context, profile, model string) (string, error) {
	installed := a.rawModels(ctx)
	if model != "" && model != "auto" && slices.Contains(installed, model) {
		return model, nil
	}
	// A bare id that isn't a model may be a profile's.
	if profile == "" && model != "auto" {
		profile = model
	}
	if a.Profiles != nil {
		if profile == "" || profile == "auto" {
			profile = a.defaultProfileID(ctx)
		}
		if p, err := a.Profiles.Get(ctx, profile); err == nil {
			for _, r := range p.Roles {
				if r.ModelID != "" && slices.Contains(installed, r.ModelID) {
					return r.ModelID, nil
				}
			}
		}
	}
	return a.defaultInstalledModelID(ctx)
}
