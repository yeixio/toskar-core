package speech

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/pyenv"
	"github.com/yeixio/toskar-core/internal/replylang"
)

//go:embed worker.py
var workerScript []byte

// Worker keeps Whisper and Piper loaded in one Python process for live
// voice mode (#795), so a turn waits only for the models, not for Python
// and the models to start, as each dictation or Read aloud job does. It
// starts on first use, starts again after a crash, and stops after it has
// been idle for IdleAfter, which frees its memory.
type Worker struct {
	Engine *Engine
	// IdleAfter is how long it's kept without a request; 0 is 10 minutes.
	IdleAfter time.Duration

	// Plain reads aloud in Piper's voices only, not Kokoro's natural ones.
	Plain bool
	// Client downloads Kokoro's model; nil is http.DefaultClient.
	Client *http.Client

	// Script replaces worker.py, for tests here and in other packages;
	// kokoroBase is Kokoro's download address, for tests.
	Script     []byte
	kokoroBase string

	// Choice is the voice an Admin chose for a language, "" for automatic
	// (#1304); nil is automatic for every language.
	Choice func(lang string) string

	mu sync.Mutex
	// procs are the speech environment's worker and Kitten's (#1304).
	procs   [envCount]*workerProc
	lastUse [envCount]time.Time
	closed  bool
	// fetching is while Kokoro downloads; kokoroFailed is when it last
	// couldn't download or speak, after which Piper reads for a while.
	fetching     bool
	kokoroFailed time.Time
	stopFetch    context.CancelFunc
	fetched      chan struct{}
	kitten       kittenJob
}

// The worker's environments: the speech environment, and Kitten's own.
const (
	envSpeech = iota
	envKitten
	envCount
)

// kokoroRetry is how long Piper reads after Kokoro fails before it's tried
// again.
const kokoroRetry = 10 * time.Minute

type workerProc struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	mu      sync.Mutex
	seq     int
	pending map[string]chan workerAnswer
	exited  chan struct{}
	stderr  *tailBuffer
}

type workerAnswer struct {
	Text     string `json:"text"`
	Language string `json:"language"`
	// Probability is how sure Whisper is of Language, from 0 to 1.
	Probability float64 `json:"probability"`
	PCM         string  `json:"pcm"`
	Rate        int     `json:"rate"`
	Error       string  `json:"error"`
	ID          string  `json:"id"`
}

// Transcribe returns what pcm, 16-bit mono at 16 kHz, says. quality is
// fast or accurate, as for Engine.Transcribe.
func (w *Worker) Transcribe(ctx context.Context, pcm []byte, quality, language string) (string, error) {
	text, _, _, err := w.Hear(ctx, pcm, quality, language)
	return text, err
}

// Hear is Transcribe, and the language it was spoken in with how sure
// Whisper is of it, from 0 to 1 (#1322). With a language given, that's
// the language, at 1.
func (w *Worker) Hear(ctx context.Context, pcm []byte, quality, language string) (text, heard string, sure float64, err error) {
	if language != "" && !languageRe.MatchString(language) {
		return "", "", 0, fmt.Errorf("language must be a code such as en or de")
	}
	model := models[quality]
	if model == "" {
		model = models["fast"]
	}
	a, err := w.ask(ctx, envSpeech, map[string]any{"op": "transcribe", "pcm": base64.StdEncoding.EncodeToString(pcm), "model": model,
		"models_dir": filepath.Join(w.Engine.Dir, "whisper"), "language": language})
	if language != "" {
		return a.Text, language, 1, err
	}
	return a.Text, a.Language, a.Probability, err
}

// Speak reads text aloud in voice, or the language's voice when it's
// empty, and returns 16-bit mono PCM at rate. Kokoro reads the languages it
// speaks, once its model is downloaded (in the background, on first use);
// Piper reads the rest, and reads while Kokoro downloads or when it fails.
func (w *Worker) Speak(ctx context.Context, text, voice, language string) (pcm []byte, rate int, err error) {
	if text == "" {
		return nil, 0, errors.New("text required")
	}
	if n := len([]rune(text)); n > maxTextRunes {
		return nil, 0, fmt.Errorf("the text is longer than %d characters", maxTextRunes)
	}
	if voice == "" && w.Choice != nil {
		lang := language
		if lang == "" {
			lang, _ = replylang.Detect(text)
		}
		if lang != "" {
			base, _, _ := strings.Cut(strings.ToLower(lang), "-")
			if chosen := w.Choice(base); chosen != "" {
				pcm, rate, err := w.Speak(ctx, text, chosen, language)
				// A chosen voice that can't read now leaves it to the
				// automatic one.
				if err == nil || ctx.Err() != nil {
					return pcm, rate, err
				}
			}
		}
	}
	if KittenVoiceRe.MatchString(voice) {
		if w.Engine.kittenInstalled() {
			return w.speakKitten(ctx, text, voice)
		}
		// Kitten was removed: the voice's language's automatic voice reads.
		voice, language = "", KittenVoiceRe.FindStringSubmatch(voice)[1]
	}
	if kokoroRe.MatchString(voice) {
		// A Kokoro voice Kokoro can't read yet: Piper reads its language.
		if w.kokoroUsable() {
			return w.speakKokoro(ctx, text, voice, true)
		}
		voice, language = "", kokoroLangs[voice[0]]
	}
	if voice == "" {
		lang := language
		if lang == "" {
			lang, _ = replylang.Detect(text)
		}
		if lang == "" {
			lang = "en"
		}
		_, kokoro := NaturalVoiceFor(lang)
		kokoro = kokoro && !w.Plain
		if kokoro && w.kokoroUsable() {
			v, _ := NaturalVoiceFor(lang)
			pcm, rate, err := w.speakKokoro(ctx, text, v, false)
			if err == nil || ctx.Err() != nil {
				return pcm, rate, err
			}
		}
		// Kitten, when installed, reads the languages Kokoro doesn't.
		if v := w.Engine.kittenSpeaks(lang); v != "" && !kokoro {
			pcm, rate, err := w.speakKitten(ctx, text, v)
			if err == nil || ctx.Err() != nil {
				return pcm, rate, err
			}
		}
	}
	voice, err = voiceFor(text, voice, language)
	if err != nil {
		return nil, 0, err
	}
	if !voiceRe.MatchString(voice) {
		return nil, 0, fmt.Errorf("%q is not a Piper or Kokoro voice name, such as %s or af_heart", voice, DefaultVoice)
	}
	a, err := w.ask(ctx, envSpeech, map[string]any{"op": "speak", "text": text, "voice": voice, "voices_dir": filepath.Join(w.Engine.Dir, "voices")})
	if err != nil {
		return nil, 0, err
	}
	return decodePCM(a)
}

// speakKokoro reads text in a Kokoro voice. A failure that isn't a cancel
// turns Kokoro off for a while; asked is whether the person chose it.
func (w *Worker) speakKokoro(ctx context.Context, text, voice string, asked bool) ([]byte, int, error) {
	dir := w.Engine.kokoroDir()
	a, err := w.ask(ctx, envSpeech, map[string]any{"op": "speak", "engine": "kokoro", "text": text, "voice": voice,
		"lang": kokoroLangs[voice[0]], "model": filepath.Join(dir, kokoroFiles[0].Name), "voices": filepath.Join(dir, kokoroFiles[1].Name)})
	if err == nil {
		return decodePCM(a)
	}
	if ctx.Err() == nil && !asked {
		w.mu.Lock()
		w.kokoroFailed = time.Now()
		w.mu.Unlock()
	}
	return nil, 0, err
}

func decodePCM(a workerAnswer) ([]byte, int, error) {
	pcm, err := base64.StdEncoding.DecodeString(a.PCM)
	if err != nil {
		return nil, 0, fmt.Errorf("speech returned something unexpected: %w", err)
	}
	return pcm, a.Rate, nil
}

// kokoroUsable is whether Kokoro can read now. When its model isn't
// downloaded, it starts downloading it and says no.
func (w *Worker) kokoroUsable() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.Plain || w.closed || w.fetching || (!w.kokoroFailed.IsZero() && time.Since(w.kokoroFailed) < kokoroRetry) {
		return false
	}
	dir := w.Engine.kokoroDir()
	if kokoroReady(dir) {
		return true
	}
	w.fetching = true
	client, base := w.Client, w.kokoroBase
	if client == nil {
		client = http.DefaultClient
	}
	if base == "" {
		base = kokoroRelease
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.stopFetch, w.fetched = cancel, make(chan struct{})
	go func(done chan struct{}) {
		defer close(done)
		defer cancel()
		err := fetchKokoro(ctx, client, base, dir)
		w.mu.Lock()
		w.fetching = false
		if err != nil {
			w.kokoroFailed = time.Now()
		}
		w.mu.Unlock()
	}(w.fetched)
	return false
}

// Close stops the worker.
func (w *Worker) Close() {
	w.mu.Lock()
	procs := w.procs
	w.procs, w.closed = [envCount]*workerProc{}, true
	stopFetch, fetched := w.stopFetch, w.fetched
	w.mu.Unlock()
	if stopFetch != nil {
		stopFetch()
		<-fetched
	}
	w.kitten.mu.Lock()
	cancel, done := w.kitten.cancel, w.kitten.done
	w.kitten.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
	for _, p := range procs {
		if p != nil {
			p.stop()
		}
	}
}

// stopEnv stops one environment's worker; it starts again when asked.
func (w *Worker) stopEnv(env int) {
	w.mu.Lock()
	p := w.procs[env]
	w.procs[env] = nil
	w.mu.Unlock()
	if p != nil {
		p.stop()
	}
}

func (w *Worker) ask(ctx context.Context, env int, req map[string]any) (workerAnswer, error) {
	p, err := w.running(ctx, env)
	if err != nil {
		return workerAnswer{}, err
	}
	p.mu.Lock()
	p.seq++
	id := strconv.Itoa(p.seq)
	answer := make(chan workerAnswer, 1)
	p.pending[id] = answer
	req["id"] = id
	line, _ := json.Marshal(req)
	_, err = p.stdin.Write(append(line, '\n'))
	p.mu.Unlock()
	if err != nil {
		p.forget(id)
		return workerAnswer{}, fmt.Errorf("speech stopped: %s", p.stderr.last(err))
	}
	select {
	case a := <-answer:
		w.touch(env)
		if a.Error != "" {
			return a, fmt.Errorf("speech failed: %s", a.Error)
		}
		return a, nil
	case <-p.exited:
		return workerAnswer{}, fmt.Errorf("speech stopped: %s", p.stderr.last(errors.New("the worker exited")))
	case <-ctx.Done():
		// The worker still answers; the answer is dropped.
		p.forget(id)
		return workerAnswer{}, ctx.Err()
	}
}

func (w *Worker) touch(env int) {
	w.mu.Lock()
	w.lastUse[env] = time.Now()
	w.mu.Unlock()
}

// running is an environment's worker process, started when there's none.
func (w *Worker) running(ctx context.Context, env int) (*workerProc, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil, errors.New("speech is stopping")
	}
	if p := w.procs[env]; p != nil {
		select {
		case <-p.exited:
			w.procs[env] = nil
		default:
			w.lastUse[env] = time.Now()
			return p, nil
		}
	}
	e := w.Engine
	var py string
	var err error
	if env == envKitten {
		// Kitten's environment is installed by InstallKitten, never here.
		st, ok := e.Python.(interface{ Status(pyenv.Spec) pyenv.Status })
		if !ok || !st.Status(KittenSpec()).Installed {
			return nil, errors.New("kitten isn't installed")
		}
		py = st.Status(KittenSpec()).Python
	} else {
		py, err = e.ensure(ctx)
	}
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(e.Dir, 0o700); err != nil {
		return nil, err
	}
	script := w.Script
	if script == nil {
		script = workerScript
	}
	path := filepath.Join(e.Dir, "worker.py")
	if err := os.WriteFile(path, script, 0o600); err != nil {
		return nil, err
	}
	cmd := exec.Command(py, "-u", path)
	cmd.Dir = e.Dir
	cmd.Env = append(append(os.Environ(), e.Python.Env()...), "HF_HOME="+filepath.Join(e.Dir, "hf"), "HF_HUB_DISABLE_TELEMETRY=1", "HF_HUB_DISABLE_PROGRESS_BARS=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	p := &workerProc{cmd: cmd, stdin: stdin, pending: map[string]chan workerAnswer{}, exited: make(chan struct{}), stderr: &tailBuffer{max: 4096}}
	cmd.Stderr = p.stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("speech couldn't start: %w", err)
	}
	go p.read(stdout)
	w.procs[env], w.lastUse[env] = p, time.Now()
	go w.stopWhenIdle(env, p)
	return p, nil
}

func (p *workerProc) read(stdout io.Reader) {
	defer func() {
		_ = p.cmd.Wait()
		close(p.exited)
	}()
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64<<10), 256<<20)
	for sc.Scan() {
		var a workerAnswer
		if json.Unmarshal(sc.Bytes(), &a) != nil {
			continue
		}
		p.mu.Lock()
		ch := p.pending[a.ID]
		delete(p.pending, a.ID)
		p.mu.Unlock()
		if ch != nil {
			ch <- a
		}
	}
}

func (p *workerProc) forget(id string) {
	p.mu.Lock()
	delete(p.pending, id)
	p.mu.Unlock()
}

func (p *workerProc) stop() {
	_ = p.stdin.Close()
	select {
	case <-p.exited:
	case <-time.After(3 * time.Second):
		_ = p.cmd.Process.Kill()
		<-p.exited
	}
}

func (w *Worker) stopWhenIdle(env int, p *workerProc) {
	idle := w.IdleAfter
	if idle <= 0 {
		idle = 10 * time.Minute
	}
	tick := idle / 4
	if tick > time.Minute {
		tick = time.Minute
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-p.exited:
			return
		case <-t.C:
		}
		w.mu.Lock()
		p.mu.Lock()
		busy := len(p.pending) > 0
		p.mu.Unlock()
		if w.procs[env] == p && !busy && time.Since(w.lastUse[env]) >= idle {
			w.procs[env] = nil
			w.mu.Unlock()
			p.stop()
			return
		}
		w.mu.Unlock()
	}
}

// tailBuffer keeps the end of the worker's error output, to say why it
// stopped.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	b   []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > t.max {
		t.b = t.b[len(t.b)-t.max:]
	}
	return len(p), nil
}

func (t *tailBuffer) last(err error) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return lastLine(string(t.b), err)
}
