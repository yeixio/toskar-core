package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	parts := voice.Parts{
		Transcriber: workerTurns{w: a.voiceWorker, quality: a.voiceHearing(ctx), language: a.voiceLanguage(ctx)},
		Responder:   &voiceChat{app: a, ctx: ctx},
		Speaker:     workerVoice{a.voiceWorker},
	}
	if a.smartTurnOn(ctx) {
		// Smart Turn's file comes down the first time, in the background
		// (#1403); until it's here the pause alone ends a turn.
		a.voiceWorker.PrepareTurnCheck()
		parts.Judge = workerJudge{w: a.voiceWorker, log: a.Logger}
	}
	return parts, nil
}

// settingSmartTurn turns on Smart Turn for voice mode and the door (#1460).
// It's off by default: in the voice bench it saved one answer in 60 from
// starting mid-sentence, and in a room with steady hiss it made answers
// about 1.8 s slower, since it heard more finished turns as unfinished.
const settingSmartTurn = "voice_smart_turn"

// smartTurnOn reports whether an Admin turned Smart Turn on.
func (a *App) smartTurnOn(ctx context.Context) bool {
	if a.Settings == nil {
		return false
	}
	on, _ := a.Settings.GetBool(ctx, settingSmartTurn, false)
	return on
}

// whisperRate is the rate Whisper listens at.
const whisperRate = 16000

// settingVoiceHearing is how voice mode and the door hear (#1330): fast,
// Whisper base, or accurate, Whisper small, which hears noticeably better
// in a noisy room or with a far microphone, for a few tenths of a second
// more per turn and about 480 MB more to download and keep in memory.
const settingVoiceHearing = "voice_hearing"

// settingVoiceLanguage is the one language voice mode and the door hear,
// for a household that always speaks it (#1405): speech to text then
// doesn't guess, and the answer follows it. "" detects each turn's.
const settingVoiceLanguage = "voice_language"

// voiceLanguage is the language voice hears in, or "" to detect it.
func (a *App) voiceLanguage(ctx context.Context) string {
	if a.Settings == nil {
		return ""
	}
	v, _ := a.Settings.GetString(ctx, settingVoiceLanguage, "")
	if !speech.Hears(v) {
		return ""
	}
	return v
}

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
	// language is the one language to hear, or "" to detect it (#1405).
	language string
}

func (t workerTurns) Transcribe(ctx context.Context, wav []byte, language string) (string, error) {
	samples, rate, err := voice.ParseWAV(wav)
	if err != nil {
		return "", err
	}
	pcm := voice.EncodePCM16(voice.Resample(samples, rate, whisperRate))
	return t.w.Transcribe(ctx, pcm, firstNonEmpty(t.quality, "fast"), firstNonEmpty(language, t.language))
}

// Hear is Transcribe with the language it heard (#1322).
func (t workerTurns) Hear(ctx context.Context, wav []byte, language string) (voice.Heard, error) {
	samples, rate, err := voice.ParseWAV(wav)
	if err != nil {
		return voice.Heard{}, err
	}
	pcm := voice.EncodePCM16(voice.Resample(samples, rate, whisperRate))
	if language == "" && t.language != "" {
		// The household's one language: heard in it, and answered in it.
		text, err := t.w.Transcribe(ctx, pcm, firstNonEmpty(t.quality, "fast"), t.language)
		return voice.Heard{Text: text, Language: t.language, Confidence: 1}, err
	}
	text, heard, sure, err := t.w.Hear(ctx, pcm, firstNonEmpty(t.quality, "fast"), language)
	return voice.Heard{Text: text, Language: heard, Confidence: sure}, err
}

// workerJudge asks Smart Turn whether a paused turn sounds finished (#1403).
// Each verdict and its time go to the log, so the voice bench can tell a
// slow check from a wrong one (#1460).
type workerJudge struct {
	w   *speech.Worker
	log *slog.Logger
}

func (j workerJudge) TurnFinished(ctx context.Context, samples []int16) (float64, error) {
	start := time.Now()
	p, err := j.w.TurnFinished(ctx, voice.EncodePCM16(voice.Resample(samples, voice.SampleRate, whisperRate)))
	if j.log != nil && !errors.Is(err, speech.ErrTurnNotReady) {
		j.log.Info("voice turn check", "finished", p, "ms", time.Since(start).Milliseconds(), "audio_ms", len(samples)*1000/voice.SampleRate, "error", err)
	}
	return p, err
}

// Warm loads Smart Turn when voice mode opens, so the first pause isn't
// judged while the model loads (#1460).
func (j workerJudge) Warm() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, _ = j.w.TurnFinished(ctx, voice.EncodePCM16(make([]int16, whisperRate)))
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
	// fillers and waits count the tool and waiting fillers said, so the
	// next is a different one.
	fillers, waits int
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
	// The goroutine keeps turn's own Done: turn is wrapped below.
	ended := turn.Done()
	go func() {
		select {
		case <-ctx.Done():
			cancel()
		case <-ended:
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
	// The answer and the fillers share delta, from three goroutines: the
	// chat's tools start on their own, and the waiting fillers keep time.
	var deltaMu sync.Mutex
	var answered, toolFilled bool
	fillers := 0
	lang := c.fillerLanguage(ctx, text)
	// say speaks a filler unless the answer has started or the turn has
	// had its share. Called with deltaMu held.
	say := func(phrase string) bool {
		if answered || fillers >= maxFillers {
			return false
		}
		fillers++
		delta(phrase + " ")
		return true
	}
	// When the answer waits on a tool, such as a web search, a short
	// spoken filler says Toskar is looking it up, as a person would,
	// instead of silence (#1487): once a turn, and only before the answer
	// has started.
	turn = turnopts.WithToolStart(turn, func(toolID string) {
		deltaMu.Lock()
		defer deltaMu.Unlock()
		if !toolFilled && say(c.filler(lang, toolID)) {
			toolFilled = true
		}
	})
	// And while it waits anyway, "Umm, just a sec." now and then, between
	// the thinking bubbles, as ChatGPT's voice does (#1487).
	waited := make(chan struct{})
	defer func() {
		deltaMu.Lock()
		answered = true
		deltaMu.Unlock()
		close(waited)
	}()
	go func() {
		wait := time.NewTimer(firstWaitFiller)
		defer wait.Stop()
		for {
			select {
			case <-waited:
				return
			case <-ctx.Done():
				return
			case <-wait.C:
			}
			deltaMu.Lock()
			ok := say(c.waitFiller(lang))
			deltaMu.Unlock()
			if !ok {
				return
			}
			wait.Reset(nextWaitFiller)
		}
	}()
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
			deltaMu.Lock()
			answered = true
			delta(chunk.Content)
			deltaMu.Unlock()
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
	d := c.app.replyLanguage(turnopts.WithSpoken(voice.WithHeardLanguage(c.ctx, voice.HeardLanguage(ctx))), id, text, "")
	switch d.Source {
	case replylang.FromRequest, replylang.FromMessage, replylang.FromSpeech, replylang.FromConversation, replylang.FromSetting:
		return d.Tag
	}
	// The person's language has no voice here: the one the answer is in.
	if d.Unspoken != "" {
		return d.Tag
	}
	return ""
}

// carStyle is how an answer asked in CarPlay is written: the phone reads
// all of it aloud to someone driving, so it must stand on its own. Without
// it, the answer came in Markdown with headings and lists, and the car said
// the rest was in Toskar on the phone.
const carStyle = `This answer is read aloud in a car, to someone driving. Give the whole answer in a few spoken sentences, and stop: when there is a lot to say, sum up what matters most rather than leave part out. Never say the rest is on their phone, in the app, or on a screen; they can ask a follow-up. No Markdown, lists, tables, headings, code blocks, emoji, or links; say a web address only if they ask for it. Spell out what a voice would stumble on, such as symbols and abbreviations.`

// watchStyle is how an answer asked on a watch is written: a few lines on
// a small screen, complete in themselves.
const watchStyle = `This answer is read on a watch's small screen. Give the whole answer in a short paragraph of plain text, a few sentences: when there is a lot to say, sum up what matters most rather than leave part out. Never say the rest is on their phone or in the app; they can ask a follow-up. No headings, tables, or code blocks.`

// glanceStyle is the style for an answer on a watch or in a car, from the
// app that asked, or "".
func glanceStyle(client string) string {
	switch client {
	case "car":
		return carStyle
	case "watch":
		return watchStyle
	}
	return ""
}

// spokenStyle is how an answer in live voice mode is written: it's heard,
// not read.
const spokenStyle = `This conversation is spoken aloud: the person talks, and your answer is read out by a voice. Answer the way a person would in conversation: a sentence or two, then stop, and let them ask for more. No Markdown, lists, tables, headings, code blocks, emoji, or links; say a web address only if they ask for it. Spell out what a voice would stumble on, such as symbols and abbreviations. Only when you looked something up, on the web or in their files, say where it's from in a few words; never say where something you already know comes from.`
