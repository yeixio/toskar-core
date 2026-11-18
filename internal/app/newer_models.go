package app

import (
	"context"
	"sort"
	"strings"

	"github.com/yeixio/toskar-core/internal/gjallarhorn"
)

// settingNotifyNewer turns the notice about newer models on or off (#593).
const settingNotifyNewer = "notify_newer_models"

// newerNotifiedKey records that a newer generation was announced, so each
// is announced once, however many sizes it has or restarts follow.
func newerNotifiedKey(family, generation string) string {
	return "models.newer_notified." + family + "@" + generation
}

// noticeNewerModels tells Admins once per new generation that an installed
// model has a newer one in its line, such as "Newer Qwen models are
// available" (#593). Only Admins and the Owner see model notices, as they
// install models. The Installed tab says which model is newer for each.
func (a *App) noticeNewerModels(ctx context.Context) {
	if a.Models == nil || a.Notifications == nil || a.Settings == nil {
		return
	}
	if on, _ := a.Settings.GetBool(ctx, settingNotifyNewer, true); !on {
		return
	}
	list, err := a.Models.List(ctx)
	if err != nil {
		return
	}
	type generation struct{ family, generation, newer, installed string }
	found := map[string]generation{}
	for _, m := range list {
		if !m.Installed || m.Newer == "" {
			continue
		}
		next, ok := a.Models.Catalog().Get(m.Newer)
		if !ok || next.Family == "" || next.Generation == "" {
			continue
		}
		key := newerNotifiedKey(next.Family, next.Generation)
		if _, done := found[key]; !done {
			found[key] = generation{next.Family, next.Generation, a.modelName(next.ID), m.DisplayName}
		}
	}
	keys := make([]string, 0, len(found))
	for k := range found {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, done, _ := a.Settings.Get(ctx, key); done {
			continue
		}
		g := found[key]
		_, err := a.Notifications.Notify(context.WithoutCancel(ctx), gjallarhorn.Request{
			SourceType: "model",
			SourceID:   g.family + "@" + g.generation,
			Category:   gjallarhorn.CategoryModel,
			Severity:   gjallarhorn.SeverityInfo,
			Message:    notice("newerModels", map[string]any{"family": familyName(g.newer)}, "newerModelsBody", map[string]any{"model": g.newer, "installed": g.installed}),
			Link:       "/models?tab=installed",
			DedupeKey:  key,
		})
		if err == nil {
			_ = a.Settings.Set(ctx, key, "true")
		}
	}
}

// familyName is a model's family as people know it, the first word of its
// name: Qwen for Qwen 3.5 4B, gpt-oss for gpt-oss 20B.
func familyName(displayName string) string {
	name, _, _ := strings.Cut(strings.TrimSpace(displayName), " ")
	return name
}
