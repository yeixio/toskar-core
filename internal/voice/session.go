// Package voice is live voice mode (#795): you talk, Toskar answers out
// loud as soon as it has a sentence, and you can interrupt it. A session
// speaks a subset of OpenAI's Realtime protocol, so the web, phone, TV,
// car, a doorbell bridge (#667), and apps written for OpenAI's voice API
// all use one standard (docs/voice.md).
//
// It's a cascade, each stage swappable: the server's turn detection (vad.go),
// speech to text, the chat model through Toskar's normal chat path (profiles,
// memory, tools), and text to speech one sentence at a time, so speaking
// starts while the answer is still being written.
package voice

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Transcriber is speech to text: a turn's audio, as a WAV file, to text.
type Transcriber interface {
	Transcribe(ctx context.Context, wav []byte, language string) (string, error)
}

// Heard is a turn's text and the language it was spoken in, with how sure
// speech to text is of it, from 0 to 1.
type Heard struct {
	Text       string
	Language   string
	Confidence float64
}

// LanguageHearer is a Transcriber that also says which language it heard
// (#1322). With language "", it detects it.
type LanguageHearer interface {
	Hear(ctx context.Context, wav []byte, language string) (Heard, error)
}

// SureOfLanguage is how sure speech to text must be of the language it
// heard for the answer to follow it. Whisper base is often unsure of a
// word or two, and then guesses wildly (#1330's bench heard Spanish as
// Thai), so an unsure guess is left to the text and the settings.
const SureOfLanguage = 0.7

type heardKey struct{}

// WithHeardLanguage says the turn was spoken in language, such as es.
func WithHeardLanguage(ctx context.Context, language string) context.Context {
	if language == "" {
		return ctx
	}
	return context.WithValue(ctx, heardKey{}, language)
}

// HeardLanguage is the language the turn was spoken in, or "".
func HeardLanguage(ctx context.Context) string {
	l, _ := ctx.Value(heardKey{}).(string)
	return l
}

// Hear transcribes wav, and says the language heard when the transcriber
// can and was sure of it, unless language sets one.
func Hear(ctx context.Context, t Transcriber, wav []byte, language string) (text, heard string, err error) {
	h, ok := t.(LanguageHearer)
	if !ok {
		text, err = t.Transcribe(ctx, wav, language)
		return text, "", err
	}
	got, err := h.Hear(ctx, wav, language)
	if err != nil {
		return "", "", err
	}
	if language == "" && got.Language != "" && got.Confidence >= SureOfLanguage {
		heard = got.Language
	}
	return got.Text, heard, nil
}

// Responder answers a turn's text, calling delta with the answer as it's
// written.
type Responder interface {
	Respond(ctx context.Context, text string, delta func(string)) error
}

// AnswerLanguager is a Responder that knows the language it will answer
// text in, such as es, because it tells the model to write in it. An
// answer is read in that language's voice, not one guessed from its first
// sentence, which a sentence such as "No tengo acceso a internet." can
// fool. "" leaves it to the guess.
type AnswerLanguager interface {
	AnswerLanguage(ctx context.Context, text string) string
}

// Speaker is text to speech: one sentence to samples at SampleRate.
type Speaker interface {
	Speak(ctx context.Context, text, voice, language string) ([]int16, error)
}

// TurnJudge says, from the audio, how likely it is that the person has
// finished what they were saying: 0 to 1 (#1403). samples are at SampleRate.
type TurnJudge interface {
	TurnFinished(ctx context.Context, samples []int16) (float64, error)
}

// Parts are a session's stages. Judge is optional: without it, or when it
// fails, the pause alone ends a turn.
type Parts struct {
	Transcriber Transcriber
	Responder   Responder
	Speaker     Speaker
	Judge       TurnJudge
}

// A pause that doesn't sound finished keeps listening this much longer
// before the turn ends anyway (#1403), and the judge gets this long to say.
const (
	unfinishedWait = 2 * time.Second
	judgeTimeout   = 1500 * time.Millisecond
	// finishedAt is the judge's answer from which a turn is over.
	finishedAt = 0.5
)

// UnfinishedWait is how much longer a pause that doesn't sound finished
// keeps listening before the turn ends anyway (#1403).
const UnfinishedWait = unfinishedWait

// SoundsUnfinished asks j whether samples, at SampleRate, end mid-thought.
// No judge, no answer in time, or an error is finished: the pause alone
// ends the turn, as before (#1403).
func SoundsUnfinished(ctx context.Context, j TurnJudge, samples []int16) bool {
	if j == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, judgeTimeout)
	defer cancel()
	p, err := j.TurnFinished(ctx, samples)
	return err == nil && p < finishedAt
}

// SessionConfig is session.update's session: what the client may set.
type SessionConfig struct {
	Voice             string         `json:"voice"`
	Instructions      string         `json:"instructions,omitempty"`
	InputAudioFormat  string         `json:"input_audio_format"`
	OutputAudioFormat string         `json:"output_audio_format"`
	TurnDetection     *TurnDetection `json:"turn_detection"`
	// InputAudioTranscription.Language is a hint for speech to text, such
	// as "de"; empty detects it.
	InputAudioTranscription *struct {
		Model    string `json:"model,omitempty"`
		Language string `json:"language,omitempty"`
	} `json:"input_audio_transcription,omitempty"`
	Modalities []string `json:"modalities,omitempty"`
}

// chunkSamples is how much audio goes in each response.audio.delta: 100 ms.
const chunkSamples = SampleRate / 10

// maxTurnSamples is the longest turn kept: two minutes.
const maxTurnSamples = 120 * SampleRate

// Session is one live voice conversation.
type Session struct {
	parts Parts
	send  func(event map[string]any) error
	now   func() time.Time

	mu       sync.Mutex
	id       string
	cfg      SessionConfig
	vad      *vad
	samples  int     // audio received so far
	buffer   []int16 // the turn being spoken, or not yet committed
	prefix   []int16 // the last moments before speech started
	inSpeech bool
	turnItem string
	// held is a turn whose pause didn't sound finished: it waits a little
	// for more words before it's committed (#1403).
	held *heldTurn
	// early is the turn being judged while its pause goes on (#1460).
	early *heldTurn
	// lastText is the last turn's transcript, for response.create.
	lastText string
	// lastHeard is the language it was spoken in, when sure, or "".
	lastHeard string
	// lastLang is the language the last answer was read in.
	lastLang string
	seq      int
	// response is the answer being given, if any.
	response *response
	// turns run one at a time, in order.
	turns chan func()
	done  chan struct{}
	once  sync.Once
}

// heldTurn is a turn that paused; a judgment or timer for one no longer
// held does nothing.
type heldTurn struct {
	item   string
	create bool
	timer  *time.Timer
	// pause is the vad's pause it was judged at, when judged early (#1460).
	pause int
	// verdict is the judge's answer so far; stopped is that the whole
	// pause went by, at endMs.
	verdict verdict
	stopped bool
	endMs   int
}

// verdict is what the judge said about a pause.
type verdict int

const (
	verdictPending verdict = iota
	verdictFinished
	verdictUnfinished
	// verdictNone is no answer in time, or an error: the pause decides.
	verdictNone
)

// response is one answer: its ids, and how to stop it.
type response struct {
	id, item string
	cancel   context.CancelFunc
	finished chan struct{}
}

// NewSession starts a session that sends its events with send, which is
// called from one goroutine at a time.
func NewSession(parts Parts, send func(event map[string]any) error) *Session {
	s := &Session{
		parts: parts,
		now:   time.Now,
		cfg:   SessionConfig{Voice: "", InputAudioFormat: "pcm16", OutputAudioFormat: "pcm16", TurnDetection: defaultTurnDetection(), Modalities: []string{"audio", "text"}},
		turns: make(chan func(), 16),
		done:  make(chan struct{}),
	}
	var sendMu sync.Mutex
	s.send = func(event map[string]any) error {
		sendMu.Lock()
		defer sendMu.Unlock()
		return send(event)
	}
	s.id = "sess_" + randomID()
	s.vad = newVAD(*s.cfg.TurnDetection)
	// A judge that can load ahead does, so the first pause isn't judged
	// while its model loads (#1460).
	if w, ok := parts.Judge.(interface{ Warm() }); ok {
		go w.Warm()
	}
	go s.runTurns()
	s.emit("session.created", map[string]any{"session": s.view()})
	return s
}

// Close ends the session, stopping any answer.
func (s *Session) Close() {
	s.once.Do(func() {
		s.mu.Lock()
		r := s.response
		s.mu.Unlock()
		if r != nil {
			r.cancel()
		}
		close(s.done)
	})
}

func (s *Session) runTurns() {
	for {
		select {
		case <-s.done:
			return
		case turn := <-s.turns:
			turn()
		}
	}
}

func (s *Session) view() map[string]any {
	return map[string]any{
		"id":                  s.id,
		"object":              "realtime.session",
		"voice":               s.cfg.Voice,
		"instructions":        s.cfg.Instructions,
		"input_audio_format":  s.cfg.InputAudioFormat,
		"output_audio_format": s.cfg.OutputAudioFormat,
		"turn_detection":      s.cfg.TurnDetection,
		"modalities":          s.cfg.Modalities,
	}
}

func (s *Session) nextID(prefix string) string {
	s.seq++
	return fmt.Sprintf("%s_%s%04d", prefix, s.id[len("sess_"):len("sess_")+6], s.seq)
}

func (s *Session) emit(kind string, fields map[string]any) {
	event := map[string]any{"type": kind}
	for k, v := range fields {
		event[k] = v
	}
	s.mu.Lock()
	event["event_id"] = s.nextID("event")
	s.mu.Unlock()
	_ = s.send(event)
}

func (s *Session) fail(code, message, eventID string) {
	e := map[string]any{"type": "invalid_request_error", "code": code, "message": message}
	if eventID != "" {
		e["event_id"] = eventID
	}
	s.emit("error", map[string]any{"error": e})
}

// Handle takes one client event, as JSON.
func (s *Session) Handle(raw []byte) {
	var in struct {
		Type    string          `json:"type"`
		EventID string          `json:"event_id"`
		Audio   string          `json:"audio"`
		Session json.RawMessage `json:"session"`
		ItemID  string          `json:"item_id"`
		EndMs   int             `json:"audio_end_ms"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		s.fail("invalid_json", "the event isn't JSON", "")
		return
	}
	switch in.Type {
	case "session.update":
		s.update(in.Session, in.EventID)
	case "input_audio_buffer.append":
		pcm, err := base64.StdEncoding.DecodeString(in.Audio)
		if err != nil || len(pcm)%2 != 0 {
			s.fail("invalid_audio", "audio is base64 pcm16", in.EventID)
			return
		}
		s.append(DecodePCM16(pcm))
	case "input_audio_buffer.commit":
		s.commit(in.EventID)
	case "input_audio_buffer.clear":
		s.mu.Lock()
		s.unhold()
		s.buffer, s.inSpeech = nil, false
		s.mu.Unlock()
		s.emit("input_audio_buffer.cleared", nil)
	case "response.create":
		s.queueResponse("")
	case "response.cancel":
		s.interrupt()
	case "conversation.item.truncate":
		// What was heard of an answer the person cut off. The chat keeps
		// the whole answer for now; the client stops playing at once.
		s.emit("conversation.item.truncated", map[string]any{"item_id": in.ItemID, "content_index": 0, "audio_end_ms": in.EndMs})
	default:
		s.fail("unknown_event", fmt.Sprintf("%q isn't an event this server takes", in.Type), in.EventID)
	}
}

func (s *Session) update(raw json.RawMessage, eventID string) {
	s.mu.Lock()
	next := s.cfg
	// turn_detection: null turns detection off, so it's told apart from
	// leaving it out.
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	if err := json.Unmarshal(raw, &next); err != nil {
		s.mu.Unlock()
		s.fail("invalid_session", "session isn't valid: "+err.Error(), eventID)
		return
	}
	if td, ok := fields["turn_detection"]; ok && string(td) == "null" {
		next.TurnDetection = nil
	} else if !ok {
		next.TurnDetection = s.cfg.TurnDetection
	}
	for _, f := range []string{next.InputAudioFormat, next.OutputAudioFormat} {
		if f != "" && f != "pcm16" {
			s.mu.Unlock()
			s.fail("unsupported_audio_format", "only pcm16 at 24 kHz is supported", eventID)
			return
		}
	}
	if next.InputAudioFormat == "" {
		next.InputAudioFormat = "pcm16"
	}
	if next.OutputAudioFormat == "" {
		next.OutputAudioFormat = "pcm16"
	}
	s.cfg = next
	if next.TurnDetection != nil {
		s.vad = newVAD(*next.TurnDetection)
	}
	view := s.view()
	s.mu.Unlock()
	s.emit("session.updated", map[string]any{"session": view})
}

func (s *Session) append(samples []int16) {
	s.mu.Lock()
	offset := s.samples
	s.samples += len(samples)
	detect := s.cfg.TurnDetection != nil && s.cfg.TurnDetection.Type == "server_vad"
	if !detect {
		s.buffer = append(s.buffer, samples...)
		s.trimBuffer()
		s.mu.Unlock()
		return
	}
	events := s.vad.feed(samples, offset)
	pad := s.cfg.TurnDetection.PrefixPaddingMs * SampleRate / 1000
	td := *s.cfg.TurnDetection
	// The audio is kept from the start of the earliest change in this
	// append, so a turn has all of its words.
	if s.inSpeech {
		s.buffer = append(s.buffer, samples...)
		s.trimBuffer()
	}
	s.mu.Unlock()
	for _, e := range events {
		switch {
		case e.started:
			s.mu.Lock()
			item := s.nextID("item")
			// From pad before speech started, out of what came before and
			// this append.
			from := e.at - pad
			all := append(append([]int16(nil), s.prefix...), samples...)
			allStart := offset - len(s.prefix)
			if from < allStart {
				from = allStart
			}
			if h := s.held; h != nil {
				// More words after a pause that didn't sound finished:
				// the same turn goes on (#1403).
				s.unhold()
				item = h.item
				s.buffer = append(s.buffer, all[from-allStart:]...)
				s.trimBuffer()
			} else {
				s.buffer = append([]int16(nil), all[from-allStart:]...)
			}
			s.turnItem, s.inSpeech = item, true
			s.mu.Unlock()
			s.emit("input_audio_buffer.speech_started", map[string]any{"audio_start_ms": ms(e.at), "item_id": item})
			// Barge-in: the person talks over the answer.
			if td.interrupts() {
				s.interrupt()
			}
		case e.paused:
			if s.parts.Judge == nil {
				continue
			}
			// Ask while the pause goes on, so the verdict is ready when the
			// pause ends (#1460).
			s.mu.Lock()
			h := &heldTurn{item: s.turnItem, create: td.creates(), pause: e.pause, endMs: ms(e.at)}
			s.early = h
			end := len(s.buffer) - (offset + len(samples) - e.at) + earlyPauseMs*SampleRate/1000
			if end > len(s.buffer) {
				end = len(s.buffer)
			}
			var audio []int16
			if end > 0 {
				audio = append([]int16(nil), s.buffer[:end]...)
			}
			s.mu.Unlock()
			go s.judgeEarly(h, audio)
		case e.stopped:
			s.mu.Lock()
			item := s.turnItem
			// Up to tailMs of the quiet after the words is kept: a word
			// that trailed off below the floor is still in it, and was cut
			// off when the turn ended at the last loud frame (#795).
			end := len(s.buffer) - (offset + len(samples) - e.at) + tailMs*SampleRate/1000
			if end > 0 && end < len(s.buffer) {
				s.buffer = s.buffer[:end]
			}
			s.inSpeech = false
			if h := s.early; h != nil && h.pause == e.pause && h.item == item {
				// Judged while the pause went on: its verdict decides, or
				// will when it comes.
				s.early = nil
				h.stopped, h.endMs = true, ms(e.at)
				s.held = h
				switch h.verdict {
				case verdictUnfinished:
					h.timer = time.AfterFunc(unfinishedWait, func() { s.release(h, h.endMs) })
					s.mu.Unlock()
				case verdictPending:
					s.mu.Unlock()
				default:
					s.mu.Unlock()
					s.release(h, h.endMs)
				}
				continue
			}
			if s.parts.Judge != nil {
				// Does it sound finished? The answer decides when the turn ends.
				h := &heldTurn{item: item, create: td.creates()}
				s.held = h
				audio := append([]int16(nil), s.buffer...)
				end := ms(e.at)
				s.mu.Unlock()
				go s.judge(h, audio, end)
				continue
			}
			s.mu.Unlock()
			s.emit("input_audio_buffer.speech_stopped", map[string]any{"audio_end_ms": ms(e.at), "item_id": item})
			s.commitTurn(item, td.creates())
		}
	}
	s.mu.Lock()
	keep := pad
	if keep < frameSamples {
		keep = frameSamples
	}
	s.prefix = append(s.prefix, samples...)
	if len(s.prefix) > keep {
		s.prefix = append([]int16(nil), s.prefix[len(s.prefix)-keep:]...)
	}
	s.mu.Unlock()
}

// judge asks whether a paused turn sounds finished: finished, or no answer
// in time, ends it now; unfinished waits a little for more words (#1403).
func (s *Session) judge(h *heldTurn, audio []int16, endMs int) {
	ctx, cancel := context.WithTimeout(context.Background(), judgeTimeout)
	p, err := s.parts.Judge.TurnFinished(ctx, audio)
	cancel()
	if err != nil || p >= finishedAt {
		s.release(h, endMs)
		return
	}
	s.mu.Lock()
	if s.held == h {
		h.timer = time.AfterFunc(unfinishedWait, func() { s.release(h, endMs) })
	}
	s.mu.Unlock()
}

// judgeEarly asks whether a turn sounds finished while its pause goes on
// (#1460), so the verdict is there when the pause ends: finished ends the
// turn then, with no wait for the judge. It doesn't end the turn sooner:
// in the voice bench, a turn ended 300 ms into a pause was answered
// mid-sentence twice as often, since "call the dentist?" sounds finished
// before "on Friday afternoon".
func (s *Session) judgeEarly(h *heldTurn, audio []int16) {
	v := verdictNone
	if len(audio) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), judgeTimeout)
		p, err := s.parts.Judge.TurnFinished(ctx, audio)
		cancel()
		switch {
		case err != nil:
		case p >= finishedAt:
			v = verdictFinished
		default:
			v = verdictUnfinished
		}
	}
	s.mu.Lock()
	h.verdict = v
	if s.held != h {
		// Still paused, or the words went on: the pause decides.
		s.mu.Unlock()
		return
	}
	// The whole pause went by while it was judged.
	if v == verdictUnfinished {
		h.timer = time.AfterFunc(unfinishedWait, func() { s.release(h, h.endMs) })
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	s.release(h, h.endMs)
}

// release commits a held turn, unless more words came first.
func (s *Session) release(h *heldTurn, endMs int) {
	s.mu.Lock()
	if s.held != h {
		s.mu.Unlock()
		return
	}
	s.unhold()
	s.mu.Unlock()
	s.emit("input_audio_buffer.speech_stopped", map[string]any{"audio_end_ms": endMs, "item_id": h.item})
	s.commitTurn(h.item, h.create)
}

// unhold drops a held turn and its timer; s.mu is held.
func (s *Session) unhold() {
	if s.held != nil && s.held.timer != nil {
		s.held.timer.Stop()
	}
	s.held = nil
	s.early = nil
}

func (s *Session) trimBuffer() {
	if len(s.buffer) > maxTurnSamples {
		s.buffer = s.buffer[len(s.buffer)-maxTurnSamples:]
	}
}

// tailMs is how much of the quiet that ended a turn is kept with it.
const tailMs = 500

func (s *Session) commit(eventID string) {
	s.mu.Lock()
	s.unhold()
	empty := len(s.buffer) == 0
	item := s.turnItem
	if item == "" || !s.inSpeech {
		item = s.nextID("item")
	}
	s.inSpeech = false
	s.mu.Unlock()
	if empty {
		s.fail("input_audio_buffer_commit_empty", "there's no audio to commit", eventID)
		return
	}
	// A client that commits itself asks for the answer with response.create.
	s.mu.Lock()
	creates := s.cfg.TurnDetection != nil && s.cfg.TurnDetection.creates()
	s.mu.Unlock()
	s.commitTurn(item, creates)
}

// commitTurn hands the buffered turn to speech to text, then, when create,
// to an answer.
func (s *Session) commitTurn(item string, create bool) {
	s.mu.Lock()
	audio := s.buffer
	s.buffer = nil
	s.turnItem = ""
	lang := ""
	if t := s.cfg.InputAudioTranscription; t != nil {
		lang = t.Language
	}
	s.mu.Unlock()
	stoppedAt := s.now()
	s.emit("input_audio_buffer.committed", map[string]any{"item_id": item})
	s.emit("conversation.item.created", map[string]any{"item": map[string]any{"id": item, "object": "realtime.item", "type": "message", "role": "user", "content": []map[string]any{{"type": "input_audio"}}}})
	s.enqueue(func() { s.turn(item, audio, lang, create, stoppedAt) })
}

func (s *Session) enqueue(turn func()) {
	select {
	case s.turns <- turn:
	case <-s.done:
	}
}

func (s *Session) turn(item string, audio []int16, lang string, create bool, stoppedAt time.Time) {
	if s.parts.Transcriber == nil {
		s.fail("speech_unavailable", "speech to text isn't set up on this computer", "")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-s.done:
			cancel()
		case <-ctx.Done():
		}
	}()
	text, heard, err := Hear(ctx, s.parts.Transcriber, WAV(audio, SampleRate), lang)
	if err != nil {
		s.emit("conversation.item.input_audio_transcription.failed", map[string]any{"item_id": item, "content_index": 0, "error": map[string]any{"type": "transcription_error", "message": err.Error()}})
		return
	}
	text = strings.TrimSpace(text)
	done := map[string]any{"item_id": item, "content_index": 0, "transcript": text}
	if heard != "" {
		// Which language it heard, so a wrong guess can be seen (#1322).
		done["language"] = heard
	}
	s.emit("conversation.item.input_audio_transcription.completed", done)
	sttMs := s.now().Sub(stoppedAt).Milliseconds()
	if text == "" {
		return
	}
	s.mu.Lock()
	s.lastText, s.lastHeard = text, heard
	s.mu.Unlock()
	if create {
		s.respond(text, stoppedAt, sttMs)
	}
}

// queueResponse answers the last transcript, for response.create.
func (s *Session) queueResponse(_ string) {
	s.enqueue(func() {
		s.mu.Lock()
		text := s.lastText
		s.mu.Unlock()
		if text == "" {
			s.fail("nothing_to_answer", "there's no turn to answer yet", "")
			return
		}
		s.respond(text, s.now(), 0)
	})
}

// interrupt stops the answer being given, if any.
func (s *Session) interrupt() {
	s.mu.Lock()
	r := s.response
	s.mu.Unlock()
	if r == nil {
		return
	}
	r.cancel()
	<-r.finished
}
