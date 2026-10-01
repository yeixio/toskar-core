package app

import (
	"context"
	"strings"

	"github.com/yeixio/yggdrasil-core/internal/tools"
)

type toolView struct {
	tools.Definition
	Enabled  bool     `json:"enabled"`
	Profiles []string `json:"profiles"`
}

func (a *App) listToolViews(ctx context.Context) (any, error) {
	disabled := a.Tools.Disabled()
	var profiles []struct {
		name  string
		tools map[string]string
	}
	if a.Profiles != nil {
		list, err := a.Profiles.List(ctx)
		if err != nil {
			return nil, err
		}
		for _, profile := range list {
			policies := map[string]string{}
			for _, tool := range profile.Tools {
				policies[tool.ToolID] = tool.Policy
			}
			profiles = append(profiles, struct {
				name  string
				tools map[string]string
			}{name: profile.Name, tools: policies})
		}
	}
	out := make([]toolView, 0)
	for _, def := range tools.Catalog() {
		_, off := disabled[def.ID]
		view := toolView{Definition: def, Enabled: !off}
		for _, profile := range profiles {
			policy := profile.tools[def.ID]
			if policy != "" && !strings.EqualFold(policy, tools.PolicyDeny) {
				view.Profiles = append(view.Profiles, profile.name)
			}
		}
		if view.Profiles == nil {
			view.Profiles = []string{}
		}
		out = append(out, view)
	}
	return out, nil
}

func (a *App) setToolEnabled(ctx context.Context, id string, enabled bool) error {
	if err := a.Tools.SetEnabled(id, enabled); err != nil {
		return err
	}
	ids := make([]string, 0)
	for disabled := range a.Tools.Disabled() {
		ids = append(ids, disabled)
	}
	return a.Settings.Set(ctx, "disabled_builtin_tools", strings.Join(ids, ","))
}

func (a *App) testTool(ctx context.Context, id string, args map[string]any) (map[string]any, error) {
	def, ok := tools.Lookup(id)
	if !ok {
		return nil, errString("unknown tool")
	}
	if def.Risk != "read" {
		return nil, errString("this tool changes data, so test it from a chat approval instead")
	}
	return a.Tools.Execute(ctx, id, args, tools.PolicyAllow, "tool test", map[string]any{"test": true})
}

func (a *App) loadDisabledTools(ctx context.Context) {
	raw, err := a.Settings.GetString(ctx, "disabled_builtin_tools", "")
	if err != nil || raw == "" {
		return
	}
	for _, id := range strings.Split(raw, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		_ = a.Tools.SetEnabled(id, false)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
