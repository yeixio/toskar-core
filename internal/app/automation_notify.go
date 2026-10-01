package app

import (
	"context"
	"fmt"

	"github.com/yeixio/yggdrasil-core/internal/automations"
	"github.com/yeixio/yggdrasil-core/internal/events"
	"github.com/yeixio/yggdrasil-core/internal/gjallarhorn"
	"github.com/yeixio/yggdrasil-core/internal/training"
)

type boolSettings interface {
	GetBool(ctx context.Context, key string, def bool) (bool, error)
}

// desktopChannel posts a notification with the operating system, while the
// user still wants desktop notices for scheduled tasks. The daemon posts it,
// so it appears while the desktop UI is closed.
type desktopChannel struct {
	settings boolSettings
	send     automations.Notifier
}

func (desktopChannel) Name() string { return "desktop" }

func (c desktopChannel) Deliver(ctx context.Context, n gjallarhorn.Notification) error {
	if c.settings != nil {
		ok, err := c.settings.GetBool(ctx, "notify_task_finish", true)
		if err != nil {
			return err
		}
		if !ok {
			return gjallarhorn.ErrSuppressed
		}
	}
	if c.send == nil {
		return gjallarhorn.ErrSuppressed
	}
	return c.send.Notify(ctx, automations.Notice{Title: n.Title, Body: n.Body})
}

// automationNotifier turns an automation's notice into a Gjallarhorn
// notification: always kept in the notification center, and posted to the
// desktop unless the user turned that off. Without a hub it posts to the
// desktop directly.
type automationNotifier struct {
	settings boolSettings
	send     automations.Notifier
	hub      *gjallarhorn.Hub
}

func (n automationNotifier) Notify(ctx context.Context, notice automations.Notice) error {
	if n.hub == nil {
		return desktopOnly(ctx, n.settings, n.send, notice)
	}
	severity := gjallarhorn.SeveritySuccess
	if notice.Failure {
		severity = gjallarhorn.SeverityError
	}
	link := "/automations"
	if notice.AutomationID != "" {
		link += "?id=" + notice.AutomationID
	}
	_, err := n.hub.Notify(ctx, gjallarhorn.Request{
		SourceType: "automation",
		SourceID:   notice.AutomationID,
		Category:   gjallarhorn.CategoryAutomation,
		Severity:   severity,
		Title:      notice.Title,
		Body:       notice.Body,
		Link:       link,
		Channels:   []string{"desktop"},
	})
	return err
}

func desktopOnly(ctx context.Context, settings boolSettings, send automations.Notifier, notice automations.Notice) error {
	if settings != nil {
		ok, err := settings.GetBool(ctx, "notify_task_finish", true)
		if err != nil {
			return err
		}
		if !ok {
			return automations.ErrNotifyDisabled
		}
	}
	if send == nil {
		return automations.ErrNotifyDisabled
	}
	return send.Notify(ctx, notice)
}

// notifyFromEvents keeps the notification center up to date with things
// that finish while nobody is looking: model downloads, training deploys,
// and pairings (Gjallarhorn §31–32). These stay in the app; they are not
// posted to the desktop.
func (a *App) notifyFromEvents(ctx context.Context) {
	if a.Notifications == nil || a.Bus == nil {
		return
	}
	id, ch := a.Bus.Subscribe()
	go func() {
		defer a.Bus.Unsubscribe(id)
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-ch:
				if !ok {
					return
				}
				if req, ok := noticeForEvent(a, evt); ok {
					if _, err := a.Notifications.Notify(context.WithoutCancel(ctx), req); err != nil && a.Logger != nil {
						a.Logger.Warn("notification failed", "event", evt.Type, "error", err)
					}
				}
			}
		}
	}()
}

func noticeForEvent(a *App, evt events.Event) (gjallarhorn.Request, bool) {
	str := func(k string) string { v, _ := evt.Payload[k].(string); return v }
	switch evt.Type {
	case events.ModelDownloadCompleted:
		name := a.modelName(str("model_id"))
		return gjallarhorn.Request{SourceType: "model", SourceID: str("model_id"), Category: gjallarhorn.CategoryModel,
			Severity: gjallarhorn.SeveritySuccess, Title: "Model ready", Body: fmt.Sprintf("%s finished downloading and is ready to use.", name),
			Link: "/models", DedupeKey: "model.download:" + str("model_id")}, true
	case events.ModelDownloadFailed:
		name := a.modelName(str("model_id"))
		return gjallarhorn.Request{SourceType: "model", SourceID: str("model_id"), Category: gjallarhorn.CategoryModel,
			Severity: gjallarhorn.SeverityError, Title: "Download failed", Body: fmt.Sprintf("%s could not be downloaded: %s", name, str("error")),
			Link: "/models", DedupeKey: "model.download:" + str("model_id")}, true
	case training.EventExportCompleted:
		return gjallarhorn.Request{SourceType: "training", SourceID: str("ai_id"), Category: gjallarhorn.CategoryTraining,
			Severity: gjallarhorn.SeveritySuccess, Title: "Model exported", Body: fmt.Sprintf("%s is ready to download as a GGUF file.", str("name")),
			Link: "/train"}, true
	case training.EventExportFailed:
		return gjallarhorn.Request{SourceType: "training", SourceID: str("ai_id"), Category: gjallarhorn.CategoryTraining,
			Severity: gjallarhorn.SeverityError, Title: "Export failed", Body: fmt.Sprintf("%s could not be exported: %s", str("name"), str("error")),
			Link: "/train"}, true
	case "training.deployed":
		return gjallarhorn.Request{SourceType: "training", SourceID: str("ai_id"), Category: gjallarhorn.CategoryTraining,
			Severity: gjallarhorn.SeveritySuccess, Title: "Specialized AI deployed", Body: "It is now in the Chat model menu and the API.",
			Link: "/train"}, true
	case events.NodePaired:
		return gjallarhorn.Request{SourceType: "node", SourceID: str("node_id"), Category: gjallarhorn.CategorySystem,
			Severity: gjallarhorn.SeveritySuccess, Title: "Computer paired", Body: "It can now run work with this computer.",
			Link: "/nodes", DedupeKey: "node.paired:" + str("node_id")}, true
	}
	return gjallarhorn.Request{}, false
}
