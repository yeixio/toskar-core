package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
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
	return a.speakAtDoor(ctx, automation, answer, false)
}

// speakAtDoor is sayAtDoor; remembered keeps what the voice read, for the
// greeting, which is the same words each time.
func (a *App) speakAtDoor(ctx context.Context, automation automations.Automation, answer string, remembered bool) (string, error) {
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
	speaker := parts.Speaker
	if remembered {
		speaker = rememberedVoice{speaker, &a.doorGreetings}
	}
	said, err := sayAnswer(ctx, speaker, a.Connectors, automation, answer, lang, time.Now())
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

// warmDoorVoice readies the voice, and for a conversation the listening,
// while the run looks at the camera and writes its greeting: started cold,
// they held Mike's greeting back about 20 s after the doorbell saw him
// (#667). What it says goes nowhere.
func warmDoorVoice(ctx context.Context, parts voice.Parts, say *automations.Say, language string) {
	if parts.Speaker != nil {
		_, _ = parts.Speaker.Speak(ctx, "Hello.", say.Voice, language)
	}
	if say.Listen && parts.Transcriber != nil {
		_, _ = parts.Transcriber.Transcribe(ctx, voice.WAV(make([]int16, voice.SampleRate/2), voice.SampleRate), language)
	}
}

// warmDoor starts warmDoorVoice for an automation that speaks.
func (a *App) warmDoor(ctx context.Context, automation automations.Automation) {
	if automation.Say == nil {
		return
	}
	parts, err := a.voiceParts(ctx)
	if err != nil {
		return
	}
	lang := a.replyLanguage(ctx, "", automation.Prompt, automation.ResponseLanguage).Tag
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		defer cancel()
		warmDoorVoice(ctx, parts, automation.Say, lang)
	}()
}

// doorGreetings are what the door says the moment it sees someone, for an
// automation that talks with the visitor, by language: Mike chose to greet
// right away rather than after the run had looked at the picture and
// written a greeting, about 20 s later (#667). Someone just passing by
// hears it too.
var doorGreetings = map[string]string{
	"en": "Hello! How can I help you?",
	"es": "¡Hola! ¿En qué puedo ayudarle?",
	"fr": "Bonjour ! Comment puis-je vous aider ?",
	"de": "Hallo! Wie kann ich Ihnen helfen?",
	"it": "Salve! Come posso aiutarla?",
	"pt": "Olá! Como posso ajudar?",
	"ja": "こんにちは。ご用件をお伺いします。",
	"ko": "안녕하세요. 무엇을 도와드릴까요?",
	"zh": "您好！请问有什么可以帮您？",
}

// doorGreeting is the greeting in language, or English.
func doorGreeting(language string) string {
	base, _, _ := strings.Cut(strings.ToLower(language), "-")
	if g, ok := doorGreetings[base]; ok {
		return g
	}
	return doorGreetings["en"]
}

// rememberedVoice keeps what it read, by words, voice and language, so the
// greeting is ready at once from the second visit on, without the voice.
type rememberedVoice struct {
	voice.Speaker
	kept *sync.Map
}

func (r rememberedVoice) Speak(ctx context.Context, text, name, language string) ([]int16, error) {
	key := text + "\x00" + name + "\x00" + language
	if pcm, ok := r.kept.Load(key); ok {
		return pcm.([]int16), nil
	}
	pcm, err := r.Speaker.Speak(ctx, text, name, language)
	if err == nil {
		r.kept.Store(key, pcm)
	}
	return pcm, err
}

// greetAtDoor greets whoever the door sees at once, then talks with them,
// for an automation that talks with the visitor: the run doesn't look at
// the picture or write a greeting first. ok is false when it didn't greet,
// in quiet hours, and the run goes on as usual.
func (a *App) greetAtDoor(ctx context.Context, automation automations.Automation, modelID string) (out automations.Execution, ok bool, err error) {
	lang := a.replyLanguage(ctx, "", automation.Prompt, automation.ResponseLanguage).Tag
	said, err := a.speakAtDoor(ctx, automation, doorGreeting(lang), true)
	if err != nil {
		return out, true, fmt.Errorf("couldn't say the answer at the door: %w", err)
	}
	if said == "" {
		return out, false, nil
	}
	out = automations.Execution{ModelID: modelID,
		Acted: []string{automations.Action{Tool: automation.Say.Connector + ".say", Args: map[string]string{"text": said}}.Describe()}}
	visit, err := a.talkAtDoor(ctx, automation, modelID, said)
	switch {
	case visit != "" && err != nil:
		out.Text = visit + "\n\n" + fmt.Sprintf("The conversation stopped: %v", err)
	case visit != "":
		out.Text = visit
	case err != nil:
		return out, true, fmt.Errorf("couldn't hear the visitor: %w", err)
	default:
		out.Text = "Someone came to the door. Toskar greeted them, and nobody answered."
	}
	return out, true, nil
}
