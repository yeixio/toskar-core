package app

import (
	"context"
	"errors"

	"github.com/yeixio/toskar-core/internal/speech"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// The Voice settings (#1304): an Admin installs Kitten TTS 2 mini and
// chooses the voice each language is read aloud in, for live voice mode,
// read-aloud on the phone and in the car, and the doorbell.

// voiceSettingKey is the setting that keeps lang's chosen voice.
func voiceSettingKey(lang string) string { return "speech_voice_" + lang }

// chosenVoice is the voice an Admin chose for lang, "" for automatic.
func (a *App) chosenVoice(lang string) string {
	if a.Settings == nil {
		return ""
	}
	v, _ := a.Settings.GetString(context.Background(), voiceSettingKey(lang), "")
	return v
}

func (a *App) speechVoices(context.Context) (speech.VoiceSettings, error) {
	if a.voiceWorker == nil {
		return speech.VoiceSettings{}, errors.New("speech isn't set up on this computer")
	}
	return a.voiceWorker.Settings(a.chosenVoice), nil
}

// setSpeechVoice chooses lang's voice; "" goes back to automatic.
func (a *App) setSpeechVoice(ctx context.Context, lang, voice string) error {
	if a.voiceWorker == nil {
		return errors.New("speech isn't set up on this computer")
	}
	if voice != "" && !a.voiceWorker.ValidVoice(lang, voice) {
		return contracts.Errorf("INVALID_VOICE", map[string]any{"language": lang, "voice": voice}, "%q isn't a voice for %s here", voice, lang)
	}
	return a.Settings.Set(ctx, voiceSettingKey(lang), voice)
}

func (a *App) installKitten(context.Context) error {
	if a.voiceWorker == nil {
		return errors.New("speech isn't set up on this computer")
	}
	return a.voiceWorker.InstallKitten()
}

func (a *App) removeKitten(context.Context) error {
	if a.voiceWorker == nil {
		return errors.New("speech isn't set up on this computer")
	}
	return a.voiceWorker.RemoveKitten()
}
