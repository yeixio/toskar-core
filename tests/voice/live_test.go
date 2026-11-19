package voicebench

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"

	"github.com/yeixio/toskar-core/internal/voice"
)

// wsLink is a running Toskar's live voice, over its WebSocket, with the
// audio sent as fast as it plays (or speed times faster).
type wsLink struct {
	conn  net.Conn
	r     io.Reader
	speed float64
	start time.Time

	pos     atomic.Int64
	mu      sync.Mutex
	evs     []event
	open    int // turns being spoken
	pending int // turns committed and not yet transcribed and answered
	done    chan struct{}
	failed  error
}

func dialLive(t *testing.T, url, key string, speed float64) *wsLink {
	h := http.Header{}
	if key != "" {
		h.Set("Authorization", "Bearer "+key)
	}
	d := ws.Dialer{Protocols: []string{"realtime"}, Header: ws.HandshakeHeaderHTTP(h), Timeout: 10 * time.Second}
	conn, br, _, err := d.Dial(context.Background(), url)
	if err != nil {
		t.Fatalf("live voice at %s: %v", url, err)
	}
	l := &wsLink{conn: conn, r: conn, speed: speed, start: time.Now(), done: make(chan struct{})}
	if br != nil {
		l.r = br
	}
	go l.read()
	return l
}

func (l *wsLink) read() {
	defer close(l.done)
	for {
		data, err := wsutil.ReadServerText(struct {
			io.Reader
			io.Writer
		}{l.r, l.conn})
		if err != nil {
			return
		}
		var e map[string]any
		if json.Unmarshal(data, &e) != nil {
			continue
		}
		kind := str(e["type"])
		l.mu.Lock()
		switch kind {
		case "input_audio_buffer.speech_started":
			l.open++
		case "input_audio_buffer.speech_stopped":
			l.open--
		case "input_audio_buffer.committed":
			l.pending++
		case "conversation.item.input_audio_transcription.completed":
			// An empty transcript isn't answered.
			if strings.TrimSpace(str(e["transcript"])) == "" {
				l.pending--
			}
		case "conversation.item.input_audio_transcription.failed":
			l.pending--
		case "response.done":
			l.pending--
		case "error":
			if l.failed == nil {
				l.failed = &liveError{e}
			}
		}
		l.evs = append(l.evs, event{Type: kind, at: int(l.pos.Load()), fields: e})
		l.mu.Unlock()
	}
}

type liveError struct{ e map[string]any }

func (e *liveError) Error() string { b, _ := json.Marshal(e.e); return string(b) }

func (l *wsLink) send(pcm []int16) {
	b, _ := json.Marshal(map[string]any{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(voice.EncodePCM16(pcm))})
	_ = wsutil.WriteClientText(l.conn, b)
	n := l.pos.Add(int64(len(pcm)))
	// Keep to the clock: a microphone sends audio as it's spoken.
	due := l.start.Add(time.Duration(float64(n) / rate / l.speed * float64(time.Second)))
	if d := time.Until(due); d > 0 {
		time.Sleep(d)
	}
}

func (l *wsLink) events() []event {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]event(nil), l.evs...)
}

func (l *wsLink) busy() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.open > 0 || l.pending > 0
}

func (l *wsLink) close() {
	_ = l.conn.Close()
	<-l.done
}

// TestLive runs the same conversations against a running Toskar's live
// voice, with its real speech to text, chat model, and voices, when
// TOSKAR_VOICE_URL is set, such as ws://127.0.0.1:7400/api/v1/voice/realtime
// (a test copy, never the Toskar you use: its chats get the answers).
// TOSKAR_VOICE_KEY is an API key, if it needs one; TOSKAR_VOICE_SPEED
// streams faster than real time, which makes the timings meaningless.
// It reports, and fails only when it can't run.
func TestLive(t *testing.T) {
	url := os.Getenv("TOSKAR_VOICE_URL")
	if url == "" {
		t.Skip("set TOSKAR_VOICE_URL to a test copy's ws://…/api/v1/voice/realtime")
	}
	speed := 1.0
	if s, err := strconv.ParseFloat(os.Getenv("TOSKAR_VOICE_SPEED"), 64); err == nil && s > 0 {
		speed = s
	}
	var err error
	clipTexts, err = loadClips("clips")
	if err != nil {
		t.Fatal(err)
	}
	var links []*wsLink
	got := bench(t, func() link {
		l := dialLive(t, url, os.Getenv("TOSKAR_VOICE_KEY"), speed)
		links = append(links, l)
		return l
	}, true)
	for _, l := range links {
		if l.failed != nil {
			t.Logf("the session sent an error: %v", l.failed)
		}
	}
	report := table(got, true)
	t.Log("\n" + report)
	if p := os.Getenv("TOSKAR_VOICE_REPORT"); p != "" {
		b, _ := json.MarshalIndent(got, "", "  ")
		_ = os.WriteFile(p, []byte(report), 0o644)
		_ = os.WriteFile(strings.TrimSuffix(p, ".md")+".json", b, 0o644)
	}
}
