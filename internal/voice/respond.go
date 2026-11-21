package voice

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/yeixio/toskar-core/internal/replylang"
)

// respond gives one answer: the chat model's text as it's written, split
// into sentences, each spoken as soon as it's complete, so the first words
// play while the rest is still being written. stoppedAt is when the person
// stopped talking, for the latency the answer reports.
func (s *Session) respond(text string, stoppedAt time.Time, sttMs int64) {
	if s.parts.Responder == nil {
		s.fail("chat_unavailable", "there's no chat model to answer with", "")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &response{cancel: cancel, finished: make(chan struct{})}
	s.mu.Lock()
	r.id, r.item = s.nextID("resp"), s.nextID("item")
	voice, lang := s.cfg.Voice, ""
	if t := s.cfg.InputAudioTranscription; t != nil {
		lang = t.Language
	}
	audio := s.speaks()
	s.response = r
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		if s.response == r {
			s.response = nil
		}
		s.mu.Unlock()
		close(r.finished)
	}()
	go func() {
		select {
		case <-s.done:
			cancel()
		case <-ctx.Done():
		}
	}()

	s.emit("response.created", map[string]any{"response": map[string]any{"id": r.id, "object": "realtime.response", "status": "in_progress"}})
	s.emit("response.output_item.added", map[string]any{"response_id": r.id, "output_index": 0, "item": map[string]any{"id": r.item, "object": "realtime.item", "type": "message", "role": "assistant", "status": "in_progress"}})

	started := s.now()
	var firstToken, firstAudio time.Time
	sentences := make(chan string, 32)
	spoken := make(chan error, 1)
	var said strings.Builder
	go func() {
		spoken <- s.speakAll(ctx, r, sentences, voice, lang, text, audio, &firstAudio, &said)
	}()

	var split splitter
	err := s.parts.Responder.Respond(ctx, text, func(delta string) {
		if delta == "" {
			return
		}
		if firstToken.IsZero() {
			firstToken = s.now()
		}
		for _, sentence := range split.add(delta) {
			select {
			case sentences <- sentence:
			case <-ctx.Done():
			}
		}
	})
	if rest := split.flush(); rest != "" && ctx.Err() == nil {
		sentences <- rest
	}
	close(sentences)
	speakErr := <-spoken

	status, reason := "completed", ""
	switch {
	case ctx.Err() != nil:
		status, reason = "cancelled", "turn_detected"
	case err != nil:
		status, reason = "failed", err.Error()
	case speakErr != nil:
		status, reason = "failed", speakErr.Error()
	}
	if audio {
		s.emit("response.audio.done", map[string]any{"response_id": r.id, "item_id": r.item, "output_index": 0, "content_index": 0})
	}
	s.emit("response.audio_transcript.done", map[string]any{"response_id": r.id, "item_id": r.item, "output_index": 0, "content_index": 0, "transcript": said.String()})
	s.emit("response.output_item.done", map[string]any{"response_id": r.id, "output_index": 0, "item": map[string]any{"id": r.item, "object": "realtime.item", "type": "message", "role": "assistant", "status": status, "content": []map[string]any{{"type": "audio", "transcript": said.String()}}}})

	// How long each stage took (#795's measurements), in the response's
	// metadata: from the end of speech to the first audio is what the
	// person waits.
	latency := map[string]any{"stt_ms": sttMs}
	if !firstToken.IsZero() {
		latency["llm_first_token_ms"] = firstToken.Sub(started).Milliseconds()
	}
	if !firstAudio.IsZero() {
		latency["first_audio_ms"] = firstAudio.Sub(started).Milliseconds()
		latency["end_to_end_ms"] = firstAudio.Sub(stoppedAt).Milliseconds()
	}
	done := map[string]any{"id": r.id, "object": "realtime.response", "status": status, "metadata": map[string]any{"toskar_latency": latency}}
	if reason != "" {
		kind := "cancelled"
		if status == "failed" {
			kind = "error"
		}
		done["status_details"] = map[string]any{"type": kind, "reason": reason}
	}
	s.emit("response.done", map[string]any{"response": done})
}

// speaks is whether answers are spoken, or text only. Called with mu held.
func (s *Session) speaks() bool {
	if len(s.cfg.Modalities) == 0 {
		return true
	}
	for _, m := range s.cfg.Modalities {
		if m == "audio" {
			return true
		}
	}
	return false
}

// speakAll speaks each sentence as it arrives, sending its words and then
// its audio in 100 ms pieces.
func (s *Session) speakAll(ctx context.Context, r *response, sentences <-chan string, voice, lang, asked string, audio bool, firstAudio *time.Time, said *strings.Builder) error {
	var failed error
	for sentence := range sentences {
		if ctx.Err() != nil || failed != nil {
			continue // drain
		}
		// One answer is read in one language's voice: chosen at its first
		// sentence, not again at each, where a short one such as "Claro."
		// can pass for another language and change the voice mid-answer.
		if lang == "" {
			lang = s.answerLanguage(sentence, asked)
		}
		if said.Len() > 0 {
			said.WriteString(" ")
		}
		said.WriteString(sentence)
		s.emit("response.audio_transcript.delta", map[string]any{"response_id": r.id, "item_id": r.item, "output_index": 0, "content_index": 0, "delta": sentence + " "})
		if !audio {
			continue
		}
		if s.parts.Speaker == nil {
			failed = errors.New("text to speech isn't set up on this computer")
			continue
		}
		pcm, err := s.parts.Speaker.Speak(ctx, spokenText(sentence), voice, lang)
		if err != nil {
			if ctx.Err() == nil {
				failed = err
			}
			continue
		}
		for i := 0; i < len(pcm) && ctx.Err() == nil; i += chunkSamples {
			end := i + chunkSamples
			if end > len(pcm) {
				end = len(pcm)
			}
			if firstAudio.IsZero() {
				*firstAudio = s.now()
			}
			s.emit("response.audio.delta", map[string]any{"response_id": r.id, "item_id": r.item, "output_index": 0, "content_index": 0, "delta": base64.StdEncoding.EncodeToString(EncodePCM16(pcm[i:end]))})
		}
	}
	return failed
}

// splitter cuts streamed text into sentences.
type splitter struct{ buf strings.Builder }

// longest is how long a piece gets before it's cut at a comma or a space,
// so a long sentence doesn't hold back the voice.
const longest = 220

func (p *splitter) add(delta string) []string {
	p.buf.WriteString(delta)
	var out []string
	for {
		text := p.buf.String()
		cut := sentenceEnd(text)
		if cut < 0 && len(text) > longest {
			cut = softBreak(text[:longest])
		}
		if cut <= 0 {
			return out
		}
		if piece := strings.TrimSpace(text[:cut]); hasWords(piece) {
			out = append(out, piece)
		}
		p.buf.Reset()
		p.buf.WriteString(text[cut:])
	}
}

func (p *splitter) flush() string {
	rest := strings.TrimSpace(p.buf.String())
	p.buf.Reset()
	if !hasWords(rest) {
		return ""
	}
	return rest
}

// sentenceEnd is just past the first sentence's end in text: . ! ? or a
// new line, followed by space, or -1 when there's none yet. "3.5" and
// "e.g." with no space after aren't ends.
func sentenceEnd(text string) int {
	for i, r := range text {
		switch r {
		case '\n':
			return i + 1
		case '.', '!', '?', '…', '。', '！', '？':
			j := i + len(string(r))
			if r == '。' || r == '！' || r == '？' {
				return j
			}
			if j < len(text) && (text[j] == ' ' || text[j] == '\n') {
				// One letter before a period is an initial or an
				// abbreviation's part, such as "U.S. "; keep going.
				if r == '.' && initial(text[:i]) {
					continue
				}
				return j
			}
		}
	}
	return -1
}

// initial is whether before ends in a word of one letter, as "U.S" or
// "J" in "J. Smith" do, or in an abbreviation such as "Dr".
func initial(before string) bool {
	word := before
	if i := strings.LastIndexAny(before, " \n"); i >= 0 {
		word = before[i+1:]
	}
	if abbreviations[strings.ToLower(word)] {
		return true
	}
	rs := []rune(before)
	n := len(rs)
	if n == 0 || !unicode.IsLetter(rs[n-1]) {
		return false
	}
	return n == 1 || rs[n-2] == ' ' || rs[n-2] == '.'
}

// abbreviations end in a period that doesn't end the sentence.
var abbreviations = map[string]bool{"mr": true, "mrs": true, "ms": true, "dr": true, "prof": true, "st": true, "jr": true, "sr": true, "vs": true, "etc": true, "e.g": true, "i.e": true}

func softBreak(text string) int {
	if i := strings.LastIndexAny(text, ",;:"); i > longest/3 {
		return i + 1
	}
	if i := strings.LastIndexByte(text, ' '); i > 0 {
		return i + 1
	}
	return len(text)
}

func hasWords(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

var (
	mdLink    = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	mdURL     = regexp.MustCompile(`https?://\S+`)
	mdMarks   = regexp.MustCompile("[*_`#>|~]+")
	mdBullets = regexp.MustCompile(`(?m)^\s*(?:[-+]|\d+\.)\s+`)
)

// spokenText is a sentence as it's said: Markdown's marks, links' addresses,
// and list bullets aren't read out.
func spokenText(s string) string {
	s = mdLink.ReplaceAllString(s, "$1")
	s = mdURL.ReplaceAllString(s, "")
	s = mdBullets.ReplaceAllString(s, "")
	s = mdMarks.ReplaceAllString(s, "")
	return strings.Join(strings.Fields(s), " ")
}

func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// answerLanguage is the language an answer is read in, from its first
// sentence when that's long enough to tell, else from what the person
// said, else the language the last answer was read in. It's kept for the
// next answer.
func (s *Session) answerLanguage(first, asked string) string {
	lang := ""
	if tag, ok := replylang.Detect(first); ok && len([]rune(first)) >= 25 {
		lang = tag
	} else if tag, ok := replylang.Detect(asked); ok {
		lang = tag
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if lang == "" {
		lang = s.lastLang
	}
	s.lastLang = lang
	return lang
}
