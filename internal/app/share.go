package app

import (
	"context"
	"fmt"
	"time"

	"github.com/yeixio/yggdrasil-core/internal/events"
	modelhealth "github.com/yeixio/yggdrasil-core/internal/models/health"
	"github.com/yeixio/yggdrasil-core/internal/share"
)

// EventWorkWaiting reports work queued behind higher-priority work (§60).
const EventWorkWaiting = "work.waiting"

// enterWork admits work to this computer by priority (spec §60). Work that
// has to wait publishes why. Without a gate, everything runs at once.
func (a *App) enterWork(ctx context.Context, class share.Class, label string, waiting func(reason string)) (*share.Work, error) {
	if a.Share == nil {
		return nil, nil
	}
	return a.Share.Enter(ctx, class, label, func(reason string) {
		if a.Logger != nil {
			a.Logger.Info("work waiting", "class", class.String(), "label", label, "reason", reason)
		}
		if a.Bus != nil {
			a.Bus.Publish(events.New(EventWorkWaiting, map[string]any{"class": class.String(), "label": label, "reason": reason}))
		}
		if waiting != nil {
			waiting(reason)
		}
	})
}

// trainingNow describes a training run holding this computer, such as
// `Training "Tire shop" is using this computer (about 12 minutes left)`.
func (a *App) trainingNow() (string, bool) {
	if a.Share == nil {
		return "", false
	}
	w, ok := a.Share.Running(share.Training)
	if !ok {
		return "", false
	}
	what := "Training"
	if w.Label() != "" {
		what = fmt.Sprintf("Training %q", w.Label())
	}
	return what + " is using this computer" + aboutLeft(w), true
}

func aboutLeft(w *share.Work) string {
	d, ok := w.Remaining()
	if !ok {
		return ""
	}
	switch m := int((d + 30*time.Second) / time.Minute); {
	case m < 1:
		return " (less than a minute left)"
	case m == 1:
		return " (about 1 minute left)"
	case m < 120:
		return fmt.Sprintf(" (about %d minutes left)", m)
	default:
		return fmt.Sprintf(" (about %d hours left)", (m+30)/60)
	}
}

// explainWhileTraining rewrites an out-of-memory failure that happened while
// training holds this computer, so the person knows why and when to retry.
func (a *App) explainWhileTraining(errText string) string {
	busy, ok := a.trainingNow()
	if !ok || !modelhealth.OutOfMemory(errText) {
		return errText
	}
	return modelhealth.WithMessage(errText, busy+", so there was not enough memory for this model. Try again when training finishes, choose a smaller model, or cancel training on the Train page.")
}
