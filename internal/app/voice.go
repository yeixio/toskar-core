package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/replylang"
	"github.com/yeixio/toskar-core/internal/speech"
	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/internal/voice"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Live voice mode (#795): its stages on this computer. Speech to text is
// Whisper and text to speech Kokoro, or Piper for the languages Kokoro
// doesn't speak, kept loaded between turns by the speech worker; the
// answer goes through the person's normal chat, so profiles, memory, and
// tools work as they do in writing, and the conversation is kept in their
// chats like any other.

func (a *App) voiceParts(ctx context.Context) (voice.Parts, error) {
	if a.Speech == nil || a.voiceWorker == nil {
		return voice.Parts{}, errors.New("speech isn't set up on this computer")
	}
	if ok, why := a.Speech.Available(); !ok {
		if why == "" {
			why = "speech isn't set up on this computer"
		}
		return voice.Parts{}, contracts.NewError("SPEECH_UNAVAILABLE", map[string]any{"detail": why}, errors.New(why))
	}
	return voice.Parts{
		Transcriber: workerTurns{w: a.voiceWorker, quality: a.voiceHearing(ctx)},
		Responder:   &voiceChat{app: a, ctx: ctx},
		Speaker:     workerVoice{a.voiceWorker},
	}, nil
}

// whisperRate is the rate Whisper listens at.
const whisperRate = 16000

// settingVoiceHearing is how voice mode and the door hear (#1330): fast,
// Whisper base, or accurate, Whisper small, which hears noticeably better
// in a noisy room or with a far microphone, for a few tenths of a second
// more per turn and about 480 MB more to download and keep in memory.
const settingVoiceHearing = "voice_hearing"

// voiceHearing is the quality voice hears with: fast or accurate.
func (a *App) voiceHearing(ctx context.Context) string {
	if a.Settings == nil {
		return "fast"
	}
	if v, _ := a.Settings.GetString(ctx, settingVoiceHearing, "fast"); v == "accurate" {
		return v
	}
	return "fast"
}

// warmHearing loads the model quality hears with, in the background, so
// the first turn after choosing it doesn't wait for its download.
func (a *App) warmHearing(quality string) {
	if a.voiceWorker == nil || a.Speech == nil {
		return
	}
	if ok, _ := a.Speech.Available(); !ok {
		return
	}
	w := a.voiceWorker
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		_, _ = w.Transcribe(ctx, make([]byte, 2*whisperRate/2), quality, "en")
	}()
}

type workerTurns struct {
	w *speech.Worker
	// quality is fast or accurate (#1330).
	quality string
}

func (t workerTurns) Transcribe(ctx context.Context, wav []byte, language string) (string, error) {
	samples, rate, err := voice.ParseWAV(wav)
	if err != nil {
		return "", err
	}
	pcm := voice.EncodePCM16(voice.Resample(samples, rate, whisperRate))
	return t.w.Transcribe(ctx, pcm, firstNonEmpty(t.quality, "fast"), language)
}

// Hear is Transcribe with the language it heard (#1322).
func (t workerTurns) Hear(ctx context.Context, wav []byte, language string) (voice.Heard, error) {
	samples, rate, err := voice.ParseWAV(wav)
	if err != nil {
		return voice.Heard{}, err
	}
	pcm := voice.EncodePCM16(voice.Resample(samples, rate, whisperRate))
	text, heard, sure, err := t.w.Hear(ctx, pcm, "fast", language)
	return voice.Heard{Text: text, Language: heard, Confidence: sure}, err
}

type workerVoice struct{ w *speech.Worker }

func (v workerVoice) Speak(ctx context.Context, text, voiceName, language string) ([]int16, error) {
	pcm, rate, err := v.w.Speak(ctx, text, voiceName, language)
	if err != nil {
		return nil, err
	}
	if rate <= 0 {
		return nil, fmt.Errorf("speech returned audio with no rate")
	}
	return voice.Resample(voice.DecodePCM16(pcm), rate, voice.SampleRate), nil
}

// voiceChat answers a session's turns in one chat of the person's, made
// at the first turn.
type voiceChat struct {
	app *App
	// ctx is the session's request's, which says who's talking.
	ctx context.Context

	mu             sync.Mutex
	conversationID string
	// saving closes once the last answer is saved.
	saving chan struct{}
}

func (c *voiceChat) Respond(ctx context.Context, text string, delta func(string)) error {
	// The last answer is saved before this one is asked, so the model hears
	// it as part of the chat.
	c.mu.Lock()
	saving := c.saving
	c.mu.Unlock()
	if saving != nil {
		select {
		case <-saving:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	c.mu.Lock()
	id := c.conversationID
	c.mu.Unlock()
	// Who's asking, from the session's request; stopped with the answer.
	turn, cancel := context.WithCancel(voice.WithHeardLanguage(context.WithoutCancel(c.ctx), voice.HeardLanguage(ctx)))
	go func() {
		select {
		case <-ctx.Done():
			cancel()
		case <-turn.Done():
		}
	}()
	if id == "" {
		conv, err := c.app.Conversations.Create(turn, "Voice chat", "", "")
		if err != nil {
			cancel()
			return err
		}
		id = conv.ID
		c.mu.Lock()
		c.conversationID = id
		c.mu.Unlock()
	}
	turn = turnopts.WithSpoken(turn)
	turn = pluginapi.WithAnswerOptions(turn, pluginapi.GenerateOptions{NoThinking: true})
	chunks, err := c.app.RunChat(turn, "", id, text, true, "", "")
	if err != nil {
		cancel()
		return err
	}
	for chunk := range chunks {
		if chunk.Error != "" {
			cancel()
			for range chunks {
			}
			return errors.New(chunk.Error)
		}
		if chunk.Content != "" {
			delta(chunk.Content)
		}
		if chunk.Done {
			// The chat saves the answer after it's done; the voice is
			// already speaking it, so that finishes here without holding
			// the turn, unless the person stops it first (#795).
			saved := make(chan struct{})
			c.mu.Lock()
			c.saving = saved
			c.mu.Unlock()
			go func() {
				for range chunks {
				}
				cancel()
				close(saved)
			}()
			return ctx.Err()
		}
	}
	cancel()
	return ctx.Err()
}

// AnswerLanguage is the language the chat tells the model to answer text
// in, when the person's words or settings say it: the voice reads the
// whole answer in it. A guess from nothing (the App or computer language
// when the person said too little to tell) is left to the answer itself.
func (c *voiceChat) AnswerLanguage(ctx context.Context, text string) string {
	c.mu.Lock()
	id := c.conversationID
	c.mu.Unlock()
	d := c.app.replyLanguage(voice.WithHeardLanguage(c.ctx, voice.HeardLanguage(ctx)), id, text, "")
	switch d.Source {
	case replylang.FromRequest, replylang.FromMessage, replylang.FromSpeech, replylang.FromConversation, replylang.FromSetting:
		return d.Tag
	}
	return ""
}

// spokenStyle is how an answer in live voice mode is written: it's heard,
// not read.
const spokenStyle = `This conversation is spoken aloud: the person talks, and your answer is read out by a voice. Answer the way a person would in conversation: a sentence or two, then stop, and let them ask for more. No Markdown, lists, tables, headings, code blocks, emoji, or links; say a web address only if they ask for it. Spell out what a voice would stumble on, such as symbols and abbreviations. Only when you looked something up, on the web or in their files, say where it's from in a few words; never say where something you already know comes from.`
