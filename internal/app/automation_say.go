package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/connectors"
	"github.com/yeixio/toskar-core/internal/egress"
	"github.com/yeixio/toskar-core/internal/voice"
)

// Speaking at the door (#667): an automation with Say speaks each run's
// answer through a camera's speaker, read by this computer's own voice, so
// whoever is at the door hears Toskar. The run is told its answer is
// spoken, and says nothing when there's nobody there, or in quiet hours.

// doorTalker speaks audio through a connected camera: connectors.Manager.
type doorTalker interface {
	Talk(ctx context.Context, service string, samples []int16, rate int) error
}

// sayAtDoor speaks answer through the automation's speaker, and returns
// what was said: empty when there was nothing to say, or it's quiet hours.
func (a *App) sayAtDoor(ctx context.Context, automation automations.Automation, answer string) (string, error) {
	if automation.Say == nil || automations.Spoken(answer) == "" {
		return "", nil
	}
	if a.Connectors == nil {
		return "", errors.New("speaking through a camera isn't available on this computer")
	}
	// Like looking, speaking through a camera is an Admin's (#667).
	if a.People != nil && automation.PersonID != "" {
		if p, err := a.People.Get(ctx, automation.PersonID); err != nil || !p.Role.AtLeast(auth.RoleAdmin) {
			return "", errors.New("speaking through a camera needs an Admin's automation")
		}
	}
	parts, err := a.voiceParts(ctx)
	if err != nil {
		return "", err
	}
	lang := a.replyLanguage(ctx, "", automation.Prompt, automation.ResponseLanguage).Tag
	said, err := sayAnswer(ctx, parts.Speaker, a.Connectors, automation, answer, lang, time.Now())
	if said != "" && a.Egress != nil {
		a.Egress.Add(ctx, egress.Connector, a.Connectors.ServiceName(automation.Say.Connector), "spoke at the door")
	}
	return said, err
}

// sayAnswer reads answer aloud and sends it to the speaker, unless it's
// nothing to say or quiet hours at now in the automation's time zone.
func sayAnswer(ctx context.Context, sp voice.Speaker, talk doorTalker, automation automations.Automation, answer, language string, now time.Time) (string, error) {
	text := automations.Spoken(answer)
	if text == "" {
		return "", nil
	}
	var loc *time.Location
	if automation.Schedule.TimeZone != "" {
		loc, _ = time.LoadLocation(automation.Schedule.TimeZone)
	}
	if automation.Say.Quiet(now, loc) {
		return "", nil
	}
	samples, err := sp.Speak(ctx, text, automation.Say.Voice, language)
	if err != nil && automation.Say.Voice != "" && ctx.Err() == nil {
		// The voice chosen for the door can't read now: the language's own does.
		samples, err = sp.Speak(ctx, text, "", language)
	}
	if err != nil {
		return "", fmt.Errorf("couldn't read the answer aloud: %w", err)
	}
	samples = voice.Resample(samples, voice.SampleRate, connectors.ReolinkTalkRate)
	if err := talk.Talk(ctx, automation.Say.Connector, samples, connectors.ReolinkTalkRate); err != nil {
		return "", err
	}
	return text, nil
}
