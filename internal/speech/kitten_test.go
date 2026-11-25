package speech

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/pyenv"
)

// fakeKitten answers as worker.py does in Kitten's environment: fetch
// writes a model config with two German voices and one English, and a
// Kitten voice answers with what it was asked to read in; Piper, in the
// speech environment, answers with its voice.
const fakeKitten = `import base64, json, os, sys
for line in sys.stdin:
    req = json.loads(line)
    if req["op"] == "fetch":
        cfg = {"languages": {"de": {}, "en": {}, "ja": {}},
               "voices": {"de": [{"key": "lena", "name": "Lena", "description": "Calm (female)"}, {"key": "jonas", "name": "Jonas"}],
                          "en": [{"key": "eleanor", "name": "Eleanor"}], "ja": [{"key": "yuki", "name": "Yuki"}]}}
        with open(os.path.join(req["dir"], "config.json"), "w") as f:
            json.dump(cfg, f)
        out = {}
    elif req.get("engine") == "kitten" and req["text"] == "kitten fails":
        out = {"error": "RuntimeError: no kitten"}
    elif req.get("engine") == "kitten":
        out = {"pcm": base64.b64encode(("kitten %s %s" % (req["voice"], req["lang"])).encode()).decode(), "rate": 24000}
    else:
        out = {"pcm": base64.b64encode(("piper " + req["voice"]).encode()).decode(), "rate": 22050}
    out["id"] = req["id"]
    print(json.dumps(out), flush=True)
`

// kittenEnv installs environments by name, as pyenv does.
type kittenEnv struct {
	py      string
	mu      sync.Mutex
	have    map[string]bool
	removed []string
}

func (e *kittenEnv) Ensure(_ context.Context, spec pyenv.Spec, _ pyenv.Progress) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.have[spec.Name] = true
	return e.py, nil
}
func (e *kittenEnv) Env() []string                 { return nil }
func (e *kittenEnv) Unavailable(pyenv.Spec) string { return "" }
func (e *kittenEnv) Status(spec pyenv.Spec) pyenv.Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	return pyenv.Status{Name: spec.Name, Installed: e.have[spec.Name], Python: e.py}
}
func (e *kittenEnv) Remove(name string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.have, name)
	e.removed = append(e.removed, name)
	return nil
}

func said(t *testing.T, w *Worker, text, voice, lang string) string {
	t.Helper()
	pcm, _, err := w.Speak(context.Background(), text, voice, lang)
	if err != nil {
		t.Fatalf("speak %q in %q: %v", text, voice, err)
	}
	return string(pcm)
}

func waitKitten(t *testing.T, w *Worker, want KittenState) KittenStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := w.KittenStatus()
		if st.State == want {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("Kitten is %+v, want %s", st, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// An Admin installs Kitten TTS 2 mini; it then reads the languages Kokoro
// doesn't, a language's chosen voice is used, a chosen voice that fails or
// was removed leaves it to the automatic one, and removing Kitten deletes
// it (#1304).
func TestKittenInstallsAndReads(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not installed")
	}
	env := &kittenEnv{py: py, have: map[string]bool{}}
	choice := map[string]string{}
	var mu sync.Mutex
	w := &Worker{Engine: &Engine{Python: env, Dir: t.TempDir()}, Script: []byte(fakeKitten), Plain: true,
		Choice: func(lang string) string { mu.Lock(); defer mu.Unlock(); return choice[lang] }}
	t.Cleanup(w.Close)
	choose := func(lang, v string) { mu.Lock(); choice[lang] = v; mu.Unlock() }

	// Not installed: a Kitten voice reads in its language's Piper voice.
	if st := w.KittenStatus(); st.State != KittenAbsent || st.Credit != "Powered by KittenML" || st.License != "Apache-2.0" {
		t.Fatalf("before install: %+v", st)
	}
	if got := said(t, w, "Hallo", "kitten:de:lena", ""); got != "piper de_DE-thorsten-medium" {
		t.Fatalf("a Kitten voice without Kitten: %q", got)
	}
	if w.ValidVoice("de", "kitten:de:lena") {
		t.Fatal("a Kitten voice could be chosen before Kitten was installed")
	}

	if err := w.InstallKitten(); err != nil {
		t.Fatal(err)
	}
	waitKitten(t, w, KittenInstalled)
	if !env.have["speech-kitten"] {
		t.Fatal("Kitten's environment wasn't installed")
	}

	// Kitten reads German, which Kokoro doesn't speak, in its first voice.
	if got := said(t, w, "Guten Tag", "", "de"); got != "kitten Lena de" {
		t.Fatalf("German: %q", got)
	}
	var de VoiceChoice
	for _, c := range w.VoiceChoices(nil) {
		if c.Language == "de" {
			de = c
		}
	}
	if de.Automatic != "kitten:de:lena" || len(de.Voices) != 3 || de.Voices[0].Engine != "kitten" || de.Voices[2].Engine != "piper" {
		t.Fatalf("German's voices: %+v", de)
	}
	// Kitten's Japanese needs a compiler to install, so it's left out.
	if w.ValidVoice("ja", "kitten:ja:yuki") {
		t.Fatal("Japanese was offered in a Kitten voice")
	}
	if !w.ValidVoice("de", "kitten:de:jonas") || w.ValidVoice("de", "kitten:en:eleanor") {
		t.Fatal("ValidVoice")
	}

	// A chosen voice is used; one that fails leaves it to the automatic.
	choose("de", "kitten:de:jonas")
	if got := said(t, w, "Guten Tag", "", "de"); got != "kitten Jonas de" {
		t.Fatalf("chosen: %q", got)
	}
	choose("de", "de_DE-thorsten-medium")
	if got := said(t, w, "Guten Tag", "", "de"); got != "piper de_DE-thorsten-medium" {
		t.Fatalf("chosen Piper: %q", got)
	}
	choose("de", "kitten:de:jonas")
	if got := said(t, w, "kitten fails", "", "de"); got != "piper de_DE-thorsten-medium" {
		t.Fatalf("a failing Kitten: %q", got)
	}

	if err := w.RemoveKitten(); err != nil {
		t.Fatal(err)
	}
	if st := w.KittenStatus(); st.State != KittenAbsent {
		t.Fatalf("after removing: %+v", st)
	}
	if _, err := os.Stat(w.Engine.kittenDir()); !os.IsNotExist(err) || len(env.removed) != 1 {
		t.Fatalf("Kitten's files stayed: %v, removed %v", err, env.removed)
	}
	// The chosen Kitten voice is gone: German's Piper voice reads.
	if got := said(t, w, "Guten Tag", "", "de"); got != "piper de_DE-thorsten-medium" {
		t.Fatalf("after removing: %q", got)
	}
}

// Kitten's voice names are its language and key.
func TestKittenVoiceNames(t *testing.T) {
	for v, ok := range map[string]bool{"kitten:en:eleanor": true, "kitten:de:lena_2": true, "kitten:english:x": false, "kitten:en:": false, "af_heart": false} {
		if KittenVoiceRe.MatchString(v) != ok {
			t.Errorf("%q: %v", v, !ok)
		}
	}
	if !strings.HasPrefix(KittenURL, "https://huggingface.co/KittenML/kitten-tts-2-mini") {
		t.Fatal(KittenURL)
	}
}
