package speech

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeWorker answers as worker.py does, without the models: a transcript
// that says how much audio it got, half a second of speech, a failure on
// "fail", a hang on "slow", and an exit on "crash".
const fakeWorker = `import base64, json, sys, time
for line in sys.stdin:
    req = json.loads(line)
    if req.get("engine") == "kokoro" and req["text"] == "kokoro fails":
        out = {"error": "ImportError: no kokoro_onnx"}
    elif req.get("engine") == "kokoro":
        out = {"pcm": base64.b64encode(("%s %s %s" % (req["voice"], req["lang"], open(req["model"]).read())).encode()).decode(), "rate": 24000}
    elif req["op"] == "transcribe":
        out = {"text": "heard %d bytes in %s" % (len(base64.b64decode(req["pcm"])), req["language"] or "auto"), "language": "en"}
    elif req.get("text") == "fail":
        out = {"error": "RuntimeError: no voice"}
    elif req.get("text") == "slow":
        time.sleep(2)
        out = {"pcm": "", "rate": 22050}
    elif req.get("text") == "crash":
        sys.stderr.write("Segmentation fault in espeak\n")
        sys.exit(3)
    else:
        out = {"pcm": base64.b64encode(b"\x01\x00" * 11025).decode(), "rate": 22050, "voice": req["voice"]}
    out["id"] = req["id"]
    print(json.dumps(out), flush=True)
`

func testWorker(t *testing.T) *Worker {
	t.Helper()
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not installed")
	}
	// Piper only, unless a test gives it a Kokoro to download.
	w := &Worker{Engine: &Engine{Python: fakeEnv{path: py}, Dir: t.TempDir()}, Script: []byte(fakeWorker), Plain: true, kokoroBase: "http://127.0.0.1:1/"}
	t.Cleanup(w.Close)
	return w
}

func TestWorkerKeepsOneProcess(t *testing.T) {
	w := testWorker(t)
	ctx := context.Background()
	text, err := w.Transcribe(ctx, make([]byte, 32000), "fast", "de")
	if err != nil || text != "heard 32000 bytes in de" {
		t.Fatalf("transcribe: %q %v", text, err)
	}
	first := w.current().cmd.Process.Pid
	pcm, rate, err := w.Speak(ctx, "Hello there.", "", "en")
	if err != nil || rate != 22050 || len(pcm) != 22050 {
		t.Fatalf("speak: %d bytes at %d, %v", len(pcm), rate, err)
	}
	if w.current().cmd.Process.Pid != first {
		t.Fatal("a second request started another process")
	}
	if _, _, err := w.Speak(ctx, "fail", "", "en"); err == nil || !strings.Contains(err.Error(), "no voice") {
		t.Fatalf("a failure: %v", err)
	}
	// The worker goes on after a failed request.
	if _, err := w.Transcribe(ctx, []byte{0, 0}, "fast", ""); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerStartsAgainAfterACrash(t *testing.T) {
	w := testWorker(t)
	ctx := context.Background()
	_, _, err := w.Speak(ctx, "crash", "", "en")
	if err == nil || !strings.Contains(err.Error(), "Segmentation fault") {
		t.Fatalf("a crash says why: %v", err)
	}
	if _, _, err := w.Speak(ctx, "Back again.", "", "en"); err != nil {
		t.Fatalf("after a crash: %v", err)
	}
}

func TestWorkerGivesUpWhenCancelled(t *testing.T) {
	w := testWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, _, err := w.Speak(ctx, "slow", "", "en"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("waited for the answer it gave up on")
	}
	// The late answer is dropped, and the next request gets its own.
	if text, err := w.Transcribe(context.Background(), []byte{0, 0}, "fast", ""); err != nil || !strings.HasPrefix(text, "heard 2 bytes") {
		t.Fatalf("after a cancel: %q %v", text, err)
	}
}

func TestWorkerStopsWhenIdle(t *testing.T) {
	w := testWorker(t)
	w.IdleAfter = 200 * time.Millisecond
	if _, err := w.Transcribe(context.Background(), []byte{0, 0}, "fast", ""); err != nil {
		t.Fatal(err)
	}
	p := w.current()
	select {
	case <-p.exited:
	case <-time.After(3 * time.Second):
		t.Fatal("still running while idle")
	}
	if _, err := w.Transcribe(context.Background(), []byte{0, 0}, "fast", ""); err != nil {
		t.Fatalf("after stopping when idle: %v", err)
	}
}

func TestWorkerChecksItsInput(t *testing.T) {
	w := &Worker{Engine: &Engine{Python: failingEnv{}, Dir: t.TempDir()}}
	if _, _, err := w.Speak(context.Background(), "hi", "not a voice", ""); err == nil {
		t.Fatal("took a voice name that isn't Piper's")
	}
	if _, err := w.Transcribe(context.Background(), nil, "fast", "english"); err == nil {
		t.Fatal("took a language that isn't a code")
	}
}

func (w *Worker) current() *workerProc {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.procs[envSpeech]
}

// fakeKokoro serves stand-ins for Kokoro's files, pinned as the real ones
// are, and counts the downloads; bad serves the wrong bytes.
func fakeKokoro(t *testing.T, w *Worker, bad bool) *atomic.Int32 {
	t.Helper()
	content := map[string]string{"model.onnx": "the model", "voices.bin": "the voices"}
	saved := kokoroFiles
	kokoroFiles = nil
	for name, body := range content {
		sum := sha256.Sum256([]byte(body))
		kokoroFiles = append(kokoroFiles, kokoroFile{name, int64(len(body)), hex.EncodeToString(sum[:])})
	}
	if kokoroFiles[0].Name != "model.onnx" {
		kokoroFiles[0], kokoroFiles[1] = kokoroFiles[1], kokoroFiles[0]
	}
	t.Cleanup(func() { kokoroFiles = saved })
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		body := content[strings.TrimPrefix(r.URL.Path, "/")]
		if bad {
			body = strings.ToUpper(body)
		}
		_, _ = rw.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	w.Plain, w.kokoroBase, w.Client = false, srv.URL+"/", srv.Client()
	return &hits
}

// waitFetched waits for Kokoro's download to end.
func waitFetched(t *testing.T, w *Worker) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		w.mu.Lock()
		busy := w.fetching
		w.mu.Unlock()
		if !busy {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("Kokoro is still downloading")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWorkerSpeaksNaturallyOnceKokoroIsDownloaded(t *testing.T) {
	w := testWorker(t)
	hits := fakeKokoro(t, w, false)
	ctx := context.Background()
	// Piper reads while Kokoro downloads.
	if _, rate, err := w.Speak(ctx, "Hello there.", "", "en"); err != nil || rate != 22050 {
		t.Fatalf("while downloading: %d %v", rate, err)
	}
	waitFetched(t, w)
	for _, c := range []struct{ text, voice, lang, want string }{
		{"Hello there.", "", "en", "af_heart en-us the model"},
		{"Cheers, mate.", "", "en-GB", "bf_emma en-gb the model"},
		{"¿Dónde está la biblioteca? Está cerca del parque.", "", "", "ef_dora es the model"},
		{"Bonjour.", "", "fr", "ff_siwis fr-fr the model"},
		{"Olá.", "", "pt-BR", "pf_dora pt-br the model"},
		{"Hello.", "bm_george", "", "bm_george en-gb the model"},
	} {
		pcm, rate, err := w.Speak(ctx, c.text, c.voice, c.lang)
		if err != nil || rate != 24000 || string(pcm) != c.want {
			t.Errorf("%q in %q: %q at %d, %v; want %q", c.text, c.lang, pcm, rate, err, c.want)
		}
	}
	// Piper reads what Kokoro doesn't speak, and the voice asked for.
	for _, c := range []struct{ voice, lang string }{{"", "de"}, {"", "zh"}, {DefaultVoice, "en"}} {
		if _, rate, err := w.Speak(ctx, "Hallo.", c.voice, c.lang); err != nil || rate != 22050 {
			t.Errorf("%s %s: %d %v", c.voice, c.lang, rate, err)
		}
	}
	if n := hits.Load(); n != 2 {
		t.Errorf("%d downloads, want one of each file", n)
	}
}

func TestWorkerFallsBackToPiper(t *testing.T) {
	w := testWorker(t)
	fakeKokoro(t, w, false)
	ctx := context.Background()
	_, _, _ = w.Speak(ctx, "Hello.", "", "en")
	waitFetched(t, w)
	// Kokoro fails: Piper reads it, and the sentences after it.
	if _, rate, err := w.Speak(ctx, "kokoro fails", "", "en"); err != nil || rate != 22050 {
		t.Fatalf("after Kokoro failed: %d %v", rate, err)
	}
	if _, rate, err := w.Speak(ctx, "Hello.", "", "en"); err != nil || rate != 22050 {
		t.Fatalf("the next sentence: %d %v", rate, err)
	}
	// A Kokoro voice asked for while Kokoro is off: Piper reads its language.
	if _, rate, err := w.Speak(ctx, "Hola.", "ef_dora", ""); err != nil || rate != 22050 {
		t.Fatalf("a Kokoro voice: %d %v", rate, err)
	}
}

func TestWorkerDoesntKeepABadDownload(t *testing.T) {
	w := testWorker(t)
	hits := fakeKokoro(t, w, true)
	ctx := context.Background()
	_, _, _ = w.Speak(ctx, "Hello.", "", "en")
	waitFetched(t, w)
	if kokoroReady(w.Engine.kokoroDir()) {
		t.Fatal("kept files that didn't match their checksums")
	}
	// It doesn't try again on every sentence.
	if _, rate, err := w.Speak(ctx, "Hello.", "", "en"); err != nil || rate != 22050 {
		t.Fatalf("after a bad download: %d %v", rate, err)
	}
	waitFetched(t, w)
	if n := hits.Load(); n != 1 {
		t.Errorf("%d downloads, want 1", n)
	}
}

func TestWorkerPlainUsesPiperOnly(t *testing.T) {
	w := testWorker(t)
	hits := fakeKokoro(t, w, false)
	w.Plain = true
	if _, rate, err := w.Speak(context.Background(), "Hello.", "", "en"); err != nil || rate != 22050 {
		t.Fatalf("plain: %d %v", rate, err)
	}
	if hits.Load() != 0 {
		t.Fatal("downloaded Kokoro for a plain worker")
	}
}
