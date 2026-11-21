package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/voice"
)

// sayTTS speaks a fifth of a second per request and remembers what it was asked.
type sayTTS struct{ text, voice, lang string }

func (s *sayTTS) Speak(_ context.Context, text, v, lang string) ([]int16, error) {
	s.text, s.voice, s.lang = text, v, lang
	if text == "fail" {
		return nil, errors.New("no voice")
	}
	return make([]int16, voice.SampleRate/5), nil
}

// An answer read in the computer's voice, a sentence at a time, for the
// phone, the car, and the TV (#795).
func TestSpeechSayAnswersAWAV(t *testing.T) {
	mgr, err := config.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tts := &sayTTS{}
	srv := NewServer(Dependencies{
		Config: mgr,
		Voice: func(context.Context) (voice.Parts, error) {
			return voice.Parts{Transcriber: voiceSTT{}, Responder: voiceLLM{}, Speaker: tts}, nil
		},
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	post := func(body any) *http.Response {
		t.Helper()
		b, _ := json.Marshal(body)
		res, err := http.Post(ts.URL+"/api/v1/speech/say", "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { res.Body.Close() })
		return res
	}

	res := post(map[string]string{"text": "  Olá, tudo bem?  ", "language": "pt-BR"})
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "audio/wav" {
		t.Fatalf("%d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	wav, _ := io.ReadAll(res.Body)
	samples, rate, err := voice.ParseWAV(wav)
	if err != nil || rate != voice.SampleRate || len(samples) != voice.SampleRate/5 {
		t.Fatalf("wav: %d samples at %d, %v", len(samples), rate, err)
	}
	if tts.text != "Olá, tudo bem?" || tts.lang != "pt-BR" || tts.voice != "" {
		t.Errorf("asked to speak %q in %q with %q", tts.text, tts.lang, tts.voice)
	}

	for _, c := range []struct {
		body any
		code string
		want int
	}{
		{map[string]string{"text": "   "}, "BAD_REQUEST", http.StatusBadRequest},
		{map[string]string{"text": strings.Repeat("a", maxSayRunes+1)}, "BAD_REQUEST", http.StatusBadRequest},
		{map[string]string{"text": "fail"}, "SPEECH_FAILED", http.StatusBadRequest},
		{"not an object", "INVALID_JSON", http.StatusBadRequest},
	} {
		res := post(c.body)
		var e struct{ Error struct{ Code string } }
		_ = json.NewDecoder(res.Body).Decode(&e)
		if res.StatusCode != c.want || e.Error.Code != c.code {
			t.Errorf("%v: %d %s, want %d %s", c.body, res.StatusCode, e.Error.Code, c.want, c.code)
		}
	}
}

func TestSpeechSayWithoutSpeech(t *testing.T) {
	mgr, err := config.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Dependencies{
		Config: mgr,
		Voice: func(context.Context) (voice.Parts, error) {
			return voice.Parts{}, errors.New("speech isn't set up on this computer")
		},
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	res, err := http.Post(ts.URL+"/api/v1/speech/say", "application/json", strings.NewReader(`{"text":"Hi."}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("%d", res.StatusCode)
	}
}
