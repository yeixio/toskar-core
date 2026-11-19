package voicebench

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/voice"
)

// The live bench's own check: over a real WebSocket to a session with
// stand-in parts, it reads transcripts, answers, their language, and a
// voice that changes mid-answer.
type standInEars struct{}

func (standInEars) Transcribe(_ context.Context, wav []byte, _ string) (string, error) {
	s, _, _ := voice.ParseWAV(wav)
	if len(s) < voice.SampleRate/2 {
		return "", nil
	}
	return "what's the weather going to be like tomorrow", nil
}

type standInChat struct{}

func (standInChat) Respond(_ context.Context, text string, delta func(string)) error {
	delta("It will be sunny tomorrow with a light breeze in the afternoon. ")
	delta("Mañana hará sol todo el día y por la tarde soplará un poco de viento.")
	return nil
}

// standInVoice reads English low and Spanish high: two voices.
type standInVoice struct{}

func (standInVoice) Speak(_ context.Context, text, _, lang string) ([]int16, error) {
	f := 110.0
	if strings.Contains(text, "Mañana") {
		f = 220
	}
	out := make([]int16, voice.SampleRate/2)
	for i := range out {
		ph := 2 * math.Pi * f * float64(i) / voice.SampleRate
		out[i] = int16(6000 * (math.Sin(ph) + 0.5*math.Sin(2*ph)))
	}
	return out, nil
}

func TestLiveHarness(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = voice.Serve(w, r, voice.Parts{Transcriber: standInEars{}, Responder: standInChat{}, Speaker: standInVoice{}})
	}))
	defer srv.Close()
	clips, err := loadClips("clips")
	if err != nil {
		t.Fatal(err)
	}
	clipTexts = clips
	l := dialLive(t, "ws"+strings.TrimPrefix(srv.URL, "http"), "", 50)
	rn := converse(l, conditions[0], clips, seed, 0)
	l.close()
	us, noise := score(rn, pad, true)
	if len(noise) != 0 {
		t.Fatalf("noise turns: %d", len(noise))
	}
	w := us[0]
	if w.ID != "weather" || w.Turns != 1 || w.Transcript == "" || w.WER > 0.3 {
		t.Fatalf("weather: %+v", w)
	}
	if w.Reply == "" || !w.VoiceChange || w.FirstAudioMs <= 0 {
		t.Fatalf("the answer: %+v", w)
	}
	cs := summarize("headset", us, noise, true)
	if cs.WERPct == 0 || cs.VoiceChanges == 0 {
		t.Fatalf("summary: %+v", cs)
	}
}
