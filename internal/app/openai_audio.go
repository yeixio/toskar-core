package app

import (
	"context"
	"errors"

	"github.com/yeixio/toskar-core/internal/api/openai"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// errSpeechSetup says how to set speech up, for /v1/audio (#1357).
var errSpeechSetup = errors.New("speech isn't set up on this computer: turn on Voice in Settings › Features, which installs it in one step")

// speechReady is nil when this computer can hear and speak, or a
// SPEECH_UNAVAILABLE error that says how to set it up.
func (a *App) speechReady() error {
	if a.Speech == nil || a.voiceWorker == nil {
		return contracts.NewError("SPEECH_UNAVAILABLE", nil, errSpeechSetup)
	}
	if ok, why := a.Speech.Available(); !ok {
		if why == "" {
			return contracts.NewError("SPEECH_UNAVAILABLE", nil, errSpeechSetup)
		}
		return contracts.NewError("SPEECH_UNAVAILABLE", map[string]any{"detail": why}, errors.New(why))
	}
	return nil
}

// openaiTranscribe answers /v1/audio/transcriptions with Whisper, at the
// quality voice mode hears with.
func (a *App) openaiTranscribe(ctx context.Context, name string, audio []byte, language string) (openai.Transcription, error) {
	if err := a.speechReady(); err != nil {
		return openai.Transcription{}, err
	}
	t, err := a.Speech.Transcribe(ctx, name, audio, a.voiceHearing(ctx), language)
	if err != nil {
		return openai.Transcription{}, err
	}
	out := openai.Transcription{Text: t.Text, Language: t.Language, Duration: t.Duration}
	for _, s := range t.Segments {
		out.Segments = append(out.Segments, openai.TranscriptSegment{Start: s.Start, End: s.End, Text: s.Text})
	}
	return out, nil
}

// openaiSpeak answers /v1/audio/speech in the voice live voice speaks with.
func (a *App) openaiSpeak(ctx context.Context, text, voiceName string) ([]int16, error) {
	if err := a.speechReady(); err != nil {
		return nil, err
	}
	return workerVoice{a.voiceWorker}.Speak(ctx, text, voiceName, "")
}
