package app

import (
	"context"

	"github.com/yeixio/toskar-core/internal/api"
)

// clearAutomationRuns removes an automation's finished runs, and with files
// the camera pictures its runs took (#1335). Chats a run was continued in
// stay: they are the person's chats now.
func (a *App) clearAutomationRuns(ctx context.Context, id string, files bool) (api.ClearedRuns, error) {
	runs, err := a.Automations.ClearRuns(ctx, id)
	if err != nil {
		return api.ClearedRuns{}, err
	}
	out := api.ClearedRuns{Runs: runs}
	if files && a.Artifacts != nil {
		n, err := a.Artifacts.DeleteBySource(ctx, "automation:"+id)
		out.Files = n
		if err != nil {
			return out, err
		}
	}
	return out, nil
}
