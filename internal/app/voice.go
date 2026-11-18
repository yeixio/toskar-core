package app

import (
	"context"
	"errors"
	"fmt"
	"sync"

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
		Transcriber: workerTurns{a.voiceWorker},
		Responder:   &voiceChat{app: a, ctx: ctx},
		Speaker:     workerVoice{a.voiceWorker},
	}, nil
}

// whisperRate is the rate Whisper listens at.
const whisperRate = 16000

type workerTurns struct{ w *speech.Worker }

func (t workerTurns) Transcribe(ctx context.Context, wav []byte, language string) (string, error) {
	samples, rate, err := voice.ParseWAV(wav)
	if err != nil {
		return "", err
	}
	pcm := voice.EncodePCM16(voice.Resample(samples, rate, whisperRate))
	return t.w.Transcribe(ctx, pcm, "fast", language)
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
}

func (c *voiceChat) Respond(ctx context.Context, text string, delta func(string)) error {
	c.mu.Lock()
	id := c.conversationID
	c.mu.Unlock()
	// Who's asking, from the session's request; stopped with the answer.
	turn, cancel := context.WithCancel(context.WithoutCancel(c.ctx))
	defer cancel()
	go func() {
		<-ctx.Done()
		cancel()
	}()
	if id == "" {
		conv, err := c.app.Conversations.Create(turn, "Voice chat", "", "")
		if err != nil {
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
		return err
	}
	for chunk := range chunks {
		if chunk.Error != "" {
			return errors.New(chunk.Error)
		}
		if chunk.Content != "" {
			delta(chunk.Content)
		}
		if chunk.Done {
			break
		}
	}
	return ctx.Err()
}

// spokenStyle is how an answer in live voice mode is written: it's heard,
// not read.
const spokenStyle = `This conversation is spoken aloud: the person talks, and your answer is read out by a voice. Answer the way a person would in conversation: a sentence or two, then stop, and let them ask for more. No Markdown, lists, tables, headings, code blocks, emoji, or links; say a web address only if they ask for it. Spell out what a voice would stumble on, such as symbols and abbreviations. When you looked something up, say where it's from in a few words.`
