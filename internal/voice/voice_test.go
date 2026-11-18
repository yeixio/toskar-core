package voice

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

func tone(seconds float64) []int16 {
	n := int(seconds * SampleRate)
	out := make([]int16, n)
	for i := range out {
		out[i] = int16(0.3 * 32767 * math.Sin(2*math.Pi*220*float64(i)/SampleRate))
	}
	return out
}

func quiet(seconds float64) []int16 {
	out := make([]int16, int(seconds*SampleRate))
	for i := range out {
		out[i] = int16((i*7919)%41 - 20) // a little room noise
	}
	return out
}

func TestVADFindsATurn(t *testing.T) {
	v := newVAD(*defaultTurnDetection())
	audio := append(append(quiet(1), tone(1)...), quiet(1)...)
	var got []vadEvent
	for i := 0; i < len(audio); i += 2400 {
		got = append(got, v.feed(audio[i:min(i+2400, len(audio))], i)...)
	}
	if len(got) != 2 || !got[0].started || !got[1].stopped {
		t.Fatalf("events: %+v", got)
	}
	if at := ms(got[0].at); at < 960 || at > 1040 {
		t.Errorf("started at %d ms, want about 1000", at)
	}
	if at := ms(got[1].at); at < 1960 || at > 2060 {
		t.Errorf("stopped at %d ms, want about 2000", at)
	}
}

func TestVADIgnoresAClick(t *testing.T) {
	v := newVAD(*defaultTurnDetection())
	audio := append(append(quiet(0.5), tone(0.03)...), quiet(1)...)
	if got := v.feed(audio, 0); len(got) != 0 {
		t.Fatalf("a 30 ms click started a turn: %+v", got)
	}
}

func TestWAVRoundTripAndResample(t *testing.T) {
	in := tone(0.1)
	out, rate, err := ParseWAV(WAV(in, SampleRate))
	if err != nil || rate != SampleRate || len(out) != len(in) || out[100] != in[100] {
		t.Fatalf("round trip: %v %d %d", err, rate, len(out))
	}
	piper := Resample(in, SampleRate, 22050)
	if back := Resample(piper, 22050, SampleRate); math.Abs(float64(len(back)-len(in))) > 2 {
		t.Fatalf("resampled %d to %d and back to %d", len(in), len(piper), len(back))
	}
}

func TestSplitterSpeaksSentenceBySentence(t *testing.T) {
	var p splitter
	var got []string
	for _, d := range []string{"Sure", "! It's 3.5 km", " away. Dr. Smith of the U.S. ", "office is in", " **Juneau**"} {
		got = append(got, p.add(d)...)
	}
	got = append(got, p.flush())
	want := []string{"Sure!", "It's 3.5 km away.", "Dr. Smith of the U.S. office is in **Juneau**"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", got)
	}
	if s := spokenText("- See **[the guide](https://toskar.ai/guide)** at https://x.y `now`"); s != "See the guide at now" {
		t.Fatalf("spoken: %q", s)
	}
}

// fakes

type fakeSTT struct {
	text string
	got  []byte
}

func (f *fakeSTT) Transcribe(_ context.Context, wav []byte, _ string) (string, error) {
	f.got = wav
	return f.text, nil
}

type fakeLLM struct {
	deltas []string
	// hold blocks after the first delta until the answer is cancelled.
	hold  bool
	asked chan string
}

func (f *fakeLLM) Respond(ctx context.Context, text string, delta func(string)) error {
	if f.asked != nil {
		f.asked <- text
	}
	for i, d := range f.deltas {
		delta(d)
		if f.hold && i == 0 {
			<-ctx.Done()
			return ctx.Err()
		}
	}
	return nil
}

type fakeTTS struct{ said []string }

func (f *fakeTTS) Speak(_ context.Context, text, _, _ string) ([]int16, error) {
	f.said = append(f.said, text)
	return tone(0.25), nil
}

type recorder struct {
	mu     sync.Mutex
	events []map[string]any
	seen   chan string
}

func newRecorder() *recorder { return &recorder{seen: make(chan string, 1000)} }

func (r *recorder) send(e map[string]any) error {
	// Through JSON, as a client gets them.
	b, _ := json.Marshal(e)
	var back map[string]any
	_ = json.Unmarshal(b, &back)
	r.mu.Lock()
	r.events = append(r.events, back)
	r.mu.Unlock()
	r.seen <- back["type"].(string)
	return nil
}

func (r *recorder) waitFor(t *testing.T, kind string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case got := <-r.seen:
			if got == kind {
				return
			}
		case <-deadline:
			t.Fatalf("no %s; got %v", kind, r.types())
		}
	}
}

func (r *recorder) types() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []string{}
	for _, e := range r.events {
		out = append(out, e["type"].(string))
	}
	return out
}

func (r *recorder) last(kind string) map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.events) - 1; i >= 0; i-- {
		if r.events[i]["type"] == kind {
			return r.events[i]
		}
	}
	return nil
}

func speak(s *Session, audio []int16) {
	for i := 0; i < len(audio); i += 2400 {
		chunk := EncodePCM16(audio[i:min(i+2400, len(audio))])
		b, _ := json.Marshal(map[string]string{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(chunk)})
		s.Handle(b)
	}
}

func TestSessionAnswersOutLoud(t *testing.T) {
	stt := &fakeSTT{text: " What's the weather? "}
	llm := &fakeLLM{deltas: []string{"It's sunny", " in Juneau. Want", " the week?"}, asked: make(chan string, 1)}
	tts := &fakeTTS{}
	rec := newRecorder()
	s := NewSession(Parts{Transcriber: stt, Responder: llm, Speaker: tts}, rec.send)
	defer s.Close()
	rec.waitFor(t, "session.created")

	speak(s, append(append(quiet(0.5), tone(1)...), quiet(0.8)...))
	rec.waitFor(t, "response.done")

	if got := <-llm.asked; got != "What's the weather?" {
		t.Fatalf("asked %q", got)
	}
	// The turn's audio: the speech, with up to 300 ms before it.
	samples, _, _ := ParseWAV(stt.got)
	if d := ms(len(samples)); d < 1000 || d > 1400 {
		t.Errorf("transcribed %d ms of audio, want the 1 s turn and its padding", d)
	}
	if strings.Join(tts.said, "|") != "It's sunny in Juneau.|Want the week?" {
		t.Errorf("spoke %q", tts.said)
	}
	order := strings.Join(rec.types(), ",")
	for _, want := range []string{"input_audio_buffer.speech_started", "input_audio_buffer.speech_stopped", "input_audio_buffer.committed", "conversation.item.input_audio_transcription.completed", "response.created", "response.audio_transcript.delta", "response.audio.delta", "response.audio.done", "response.done"} {
		i := strings.Index(order, want)
		if i < 0 {
			t.Fatalf("no %s in %s", want, order)
		}
		order = order[i+len(want):]
	}
	done := rec.last("response.done")["response"].(map[string]any)
	if done["status"] != "completed" {
		t.Fatalf("response.done: %v", done)
	}
	latency := done["metadata"].(map[string]any)["toskar_latency"].(map[string]any)
	for _, k := range []string{"stt_ms", "llm_first_token_ms", "first_audio_ms", "end_to_end_ms"} {
		if _, ok := latency[k]; !ok {
			t.Errorf("latency has no %s: %v", k, latency)
		}
	}
	// 0.25 s per sentence in 100 ms pieces: 3 + 3.
	n := 0
	for _, k := range rec.types() {
		if k == "response.audio.delta" {
			n++
		}
	}
	if n != 6 {
		t.Errorf("%d audio deltas, want 6", n)
	}
}

func TestSessionStopsWhenInterrupted(t *testing.T) {
	llm := &fakeLLM{deltas: []string{"Let me tell you a long story. ", "Once upon a time."}, hold: true}
	rec := newRecorder()
	s := NewSession(Parts{Transcriber: &fakeSTT{text: "tell me a story"}, Responder: llm, Speaker: &fakeTTS{}}, rec.send)
	defer s.Close()
	speak(s, append(append(quiet(0.3), tone(0.6)...), quiet(0.7)...))
	rec.waitFor(t, "response.audio.delta")
	// The person talks over it.
	speak(s, tone(0.3))
	rec.waitFor(t, "response.done")
	done := rec.last("response.done")["response"].(map[string]any)
	if done["status"] != "cancelled" {
		t.Fatalf("response.done: %v", done)
	}
	if strings.Contains(rec.last("response.audio_transcript.done")["transcript"].(string), "Once upon") {
		t.Fatal("kept talking after the interruption")
	}
}

func TestSessionManualTurns(t *testing.T) {
	rec := newRecorder()
	s := NewSession(Parts{Transcriber: &fakeSTT{text: "hi"}, Responder: &fakeLLM{deltas: []string{"Hello!"}}, Speaker: &fakeTTS{}}, rec.send)
	defer s.Close()
	s.Handle([]byte(`{"type":"session.update","session":{"turn_detection":null,"modalities":["text"]}}`))
	rec.waitFor(t, "session.updated")
	if td := rec.last("session.updated")["session"].(map[string]any)["turn_detection"]; td != nil {
		t.Fatalf("turn detection still on: %v", td)
	}
	speak(s, tone(0.5))
	s.Handle([]byte(`{"type":"input_audio_buffer.commit"}`))
	rec.waitFor(t, "conversation.item.input_audio_transcription.completed")
	s.Handle([]byte(`{"type":"response.create"}`))
	rec.waitFor(t, "response.done")
	for _, k := range rec.types() {
		if k == "response.audio.delta" || k == "input_audio_buffer.speech_started" {
			t.Fatalf("%s with detection off and text only: %v", k, rec.types())
		}
	}
	if got := rec.last("response.audio_transcript.done")["transcript"]; got != "Hello!" {
		t.Fatalf("transcript %v", got)
	}
	s.Handle([]byte(`{"type":"session.update","session":{"input_audio_format":"g711_ulaw"}}`))
	rec.waitFor(t, "error")
}
