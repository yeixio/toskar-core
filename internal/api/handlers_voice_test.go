package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/voice"
)

type voiceSTT struct{}

func (voiceSTT) Transcribe(context.Context, []byte, string) (string, error) { return "hello", nil }

type voiceLLM struct{}

func (voiceLLM) Respond(_ context.Context, text string, delta func(string)) error {
	delta("Hi! You said " + text + ".")
	return nil
}

type voiceTTS struct{}

func (voiceTTS) Speak(context.Context, string, string, string) ([]int16, error) {
	return make([]int16, voice.SampleRate/5), nil
}

// Live voice over a real WebSocket, end to end with stand-in stages (#795).
func TestVoiceRealtimeWebSocket(t *testing.T) {
	mgr, err := config.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Dependencies{
		Config: mgr,
		Voice: func(context.Context) (voice.Parts, error) {
			return voice.Parts{Transcriber: voiceSTT{}, Responder: voiceLLM{}, Speaker: voiceTTS{}}, nil
		},
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Without an upgrade, a coded error.
	res, err := http.Get(ts.URL + "/api/v1/voice/realtime")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUpgradeRequired {
		t.Fatalf("plain GET: %d", res.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dialer := ws.Dialer{Protocols: []string{"realtime"}}
	conn, _, hs, err := dialer.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/api/v1/voice/realtime")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if hs.Protocol != "realtime" {
		t.Errorf("subprotocol %q", hs.Protocol)
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	read := func() map[string]any {
		t.Helper()
		data, _, err := wsutil.ReadServerData(conn)
		if err != nil {
			t.Fatal(err)
		}
		var e map[string]any
		_ = json.Unmarshal(data, &e)
		return e
	}
	if e := read(); e["type"] != "session.created" {
		t.Fatalf("first event: %v", e)
	}
	// Half a second of quiet, a second of speech, and the quiet after.
	audio := make([]int16, 0, 3*voice.SampleRate)
	for i := 0; i < 3*voice.SampleRate; i++ {
		v := 0.0
		if i >= voice.SampleRate/2 && i < 3*voice.SampleRate/2 {
			v = 0.3 * math.Sin(2*math.Pi*220*float64(i)/voice.SampleRate)
		}
		audio = append(audio, int16(v*32767))
	}
	for i := 0; i < len(audio); i += 4800 {
		b, _ := json.Marshal(map[string]string{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(voice.EncodePCM16(audio[i:min(i+4800, len(audio))]))})
		if err := wsutil.WriteClientMessage(conn, ws.OpText, b); err != nil {
			t.Fatal(err)
		}
	}
	var seen []string
	for {
		e := read()
		seen = append(seen, e["type"].(string))
		if e["type"] == "conversation.item.input_audio_transcription.completed" && e["transcript"] != "hello" {
			t.Fatalf("transcript: %v", e)
		}
		if e["type"] == "response.done" {
			if st := e["response"].(map[string]any)["status"]; st != "completed" {
				t.Fatalf("response.done: %v", e)
			}
			break
		}
	}
	all := strings.Join(seen, " ")
	for _, want := range []string{"input_audio_buffer.speech_started", "input_audio_buffer.speech_stopped", "response.audio_transcript.delta", "response.audio.delta"} {
		if !strings.Contains(all, want) {
			t.Errorf("no %s in %s", want, all)
		}
	}
}
