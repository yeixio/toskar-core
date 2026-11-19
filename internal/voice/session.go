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

// Responder answers a turn's text, calling delta with the answer as it's
// written.
type Responder interface {
	Respond(ctx context.Context, text string, delta func(string)) error
}

// Speaker is text to speech: one sentence to samples at SampleRate.
type Speaker interface {
	Speak(ctx context.Context, text, voice, language string) ([]int16, error)
}

// Parts are a session's stages.
type Parts struct {
	Transcriber Transcriber
	Responder   Responder
	Speaker     Speaker
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
	// lastText is the last turn's transcript, for response.create.
	lastText string
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
			s.turnItem, s.inSpeech = item, true
			// From pad before speech started, out of what came before and
			// this append.
			from := e.at - pad
			all := append(append([]int16(nil), s.prefix...), samples...)
			allStart := offset - len(s.prefix)
			if from < allStart {
				from = allStart
			}
			s.buffer = append([]int16(nil), all[from-allStart:]...)
			s.mu.Unlock()
			s.emit("input_audio_buffer.speech_started", map[string]any{"audio_start_ms": ms(e.at), "item_id": item})
			// Barge-in: the person talks over the answer.
			if td.interrupts() {
				s.interrupt()
			}
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

func (s *Session) trimBuffer() {
	if len(s.buffer) > maxTurnSamples {
		s.buffer = s.buffer[len(s.buffer)-maxTurnSamples:]
	}
}

// tailMs is how much of the quiet that ended a turn is kept with it.
const tailMs = 500

func (s *Session) commit(eventID string) {
	s.mu.Lock()
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
	text, err := s.parts.Transcriber.Transcribe(ctx, WAV(audio, SampleRate), lang)
	if err != nil {
		s.emit("conversation.item.input_audio_transcription.failed", map[string]any{"item_id": item, "content_index": 0, "error": map[string]any{"type": "transcription_error", "message": err.Error()}})
		return
	}
	text = strings.TrimSpace(text)
	s.emit("conversation.item.input_audio_transcription.completed", map[string]any{"item_id": item, "content_index": 0, "transcript": text})
	sttMs := s.now().Sub(stoppedAt).Milliseconds()
	if text == "" {
		return
	}
	s.mu.Lock()
	s.lastText = text
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
