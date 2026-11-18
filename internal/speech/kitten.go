package speech

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/yeixio/toskar-core/internal/pyenv"
)

// Kitten TTS 2 mini (#1304) is a voice engine an Admin chooses to install:
// 16 languages and 118 voices, many in languages where Piper sounds flat.
// The model, its voices, and the kittenml package are Apache-2.0; the
// larger Kitten TTS 2 is under another license and isn't offered (#1319).
// It runs in its own Python environment, since kittenml needs PyTorch for
// its English pronunciation, which the speech environment doesn't carry.

// KittenRepo is the model's Hugging Face repository, and kittenRevision the
// commit it's downloaded at, so what's installed is what was tested.
const (
	KittenRepo     = "KittenML/kitten-tts-2-mini"
	kittenRevision = "main"
)

// KittenCredit is the credit shown wherever a Kitten voice is used.
const KittenCredit = "Powered by KittenML"

// KittenURL is the model's page, with its license.
const KittenURL = "https://huggingface.co/" + KittenRepo

// KittenRequirements are the Kitten environment's packages. kittenml's
// English needs spaCy's small English model, which it would otherwise
// download with pip on first use, which the environment hasn't got.
var KittenRequirements = []string{
	"kittenml==0.9.8",
	"en_core_web_sm @ https://github.com/explosion/spacy-models/releases/download/en_core_web_sm-3.8.0/en_core_web_sm-3.8.0-py3-none-any.whl",
}

// KittenSpec is Kitten's Python environment. PyTorch's CPU build is enough
// for the pronunciation model, and much smaller.
func KittenSpec() pyenv.Spec {
	return pyenv.Spec{Name: "speech-kitten", Requirements: KittenRequirements, InstallArgs: []string{"--torch-backend=cpu"}}
}

// kittenFiles are the files downloaded: the 8-bit model, the voices, and
// the pronunciation files, not the 327 MB PyTorch weights.
var kittenFiles = []string{"config.json", "vocab.json", "*.onnx", "voices_*.npz", "g2p/*", "g2p/**", "LICENSE*", "README.md"}

// KittenVoiceRe is a Kitten voice, such as kitten:en:eleanor: its language
// and its key in the model's voices.
var KittenVoiceRe = regexp.MustCompile(`^kitten:([a-z]{2}):([a-z0-9_]{1,40})$`)

// KittenVoiceID is the voice name for a Kitten voice in lang.
func KittenVoiceID(lang, key string) string { return "kitten:" + lang + ":" + key }

// kittenDir holds the downloaded model.
func (e *Engine) kittenDir() string { return filepath.Join(e.Dir, "kitten-2-mini") }

// kittenReadyFile marks a finished download, with its revision.
func (e *Engine) kittenReadyFile() string { return filepath.Join(e.kittenDir(), ".toskar-ready") }

// KittenVoice is one of the model's voices.
type KittenVoice struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Language    string `json:"language"`
}

type kittenConfig struct {
	Languages map[string]json.RawMessage `json:"languages"`
	Voices    map[string][]struct {
		Key         string `json:"key"`
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"voices"`
}

// kittenVoices reads the model's voices, by language; nil when it isn't
// installed.
func (e *Engine) kittenVoices() map[string][]KittenVoice {
	if !e.kittenInstalled() {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(e.kittenDir(), "config.json"))
	if err != nil {
		return nil
	}
	var c kittenConfig
	if json.Unmarshal(raw, &c) != nil {
		return nil
	}
	out := map[string][]KittenVoice{}
	for lang := range c.Languages {
		for _, v := range c.Voices[lang] {
			if !KittenVoiceRe.MatchString(KittenVoiceID(lang, v.Key)) {
				continue
			}
			out[lang] = append(out[lang], KittenVoice{ID: KittenVoiceID(lang, v.Key), Name: v.Name, Description: v.Description, Language: lang})
		}
	}
	return out
}

// kittenInstalled is whether the environment and the model are both here.
func (e *Engine) kittenInstalled() bool {
	b, err := os.ReadFile(e.kittenReadyFile())
	if err != nil || strings.TrimSpace(string(b)) != kittenRevision {
		return false
	}
	st, ok := e.Python.(interface{ Status(pyenv.Spec) pyenv.Status })
	return ok && st.Status(KittenSpec()).Installed
}

// kittenSpeaks is the Kitten voice for lang, the first of its voices, or
// "" when Kitten isn't installed or doesn't speak it.
func (e *Engine) kittenSpeaks(lang string) string {
	base, _, _ := strings.Cut(strings.ToLower(lang), "-")
	vs := e.kittenVoices()[base]
	if len(vs) == 0 {
		return ""
	}
	return vs[0].ID
}

// KittenState is where Kitten's install is.
type KittenState string

const (
	KittenAbsent     KittenState = "not_installed"
	KittenInstalling KittenState = "installing"
	KittenInstalled  KittenState = "installed"
	KittenFailed     KittenState = "failed"
)

// KittenStatus is Kitten's install, for the Voice settings.
type KittenStatus struct {
	State KittenState `json:"state"`
	// Stage is what an install is doing: program or voices.
	Stage string `json:"stage,omitempty"`
	Error string `json:"error,omitempty"`
	// Unavailable is why it can't be installed here.
	Unavailable string `json:"unavailable,omitempty"`
	Credit      string `json:"credit"`
	URL         string `json:"url"`
	License     string `json:"license"`
	// Languages are the languages it speaks once installed.
	Languages []string `json:"languages"`
}

// kittenLanguages are the languages Kitten TTS 2 mini speaks.
var kittenLanguages = strings.Fields("da de en es fi fr hi it ja ko nl pt ru sv sw tr")

// kittenJob is an install in progress, or the last one's failure.
type kittenJob struct {
	mu      sync.Mutex
	running bool
	stage   string
	err     string
	cancel  context.CancelFunc
	done    chan struct{}
}

// KittenStatus reports Kitten's install.
func (w *Worker) KittenStatus() KittenStatus {
	st := KittenStatus{State: KittenAbsent, Credit: KittenCredit, URL: KittenURL, License: "Apache-2.0", Languages: kittenLanguages}
	j := &w.kitten
	j.mu.Lock()
	defer j.mu.Unlock()
	switch {
	case j.running:
		st.State, st.Stage = KittenInstalling, j.stage
	case w.Engine.kittenInstalled():
		st.State = KittenInstalled
	case j.err != "":
		st.State, st.Error = KittenFailed, j.err
	}
	if st.State != KittenInstalled {
		st.Unavailable = w.Engine.Python.Unavailable(KittenSpec())
	}
	return st
}

// InstallKitten starts installing Kitten in the background: its Python
// environment, then the model. It does nothing while an install runs.
func (w *Worker) InstallKitten() error {
	if why := w.Engine.Python.Unavailable(KittenSpec()); why != "" {
		return errors.New(why)
	}
	j := &w.kitten
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.running || w.Engine.kittenInstalled() {
		return nil
	}
	w.mu.Lock()
	closed := w.closed
	w.mu.Unlock()
	if closed {
		return errors.New("speech is stopping")
	}
	ctx, cancel := context.WithCancel(context.Background())
	j.running, j.stage, j.err, j.cancel, j.done = true, "program", "", cancel, make(chan struct{})
	go func(done chan struct{}) {
		defer close(done)
		defer cancel()
		err := w.installKitten(ctx, func(stage string) {
			j.mu.Lock()
			j.stage = stage
			j.mu.Unlock()
		})
		j.mu.Lock()
		j.running, j.stage = false, ""
		if err != nil {
			j.err = err.Error()
		}
		j.mu.Unlock()
	}(j.done)
	return nil
}

func (w *Worker) installKitten(ctx context.Context, stage func(string)) error {
	if _, err := w.Engine.Python.Ensure(ctx, KittenSpec(), nil); err != nil {
		return fmt.Errorf("couldn't install Kitten's program: %w", err)
	}
	stage("voices")
	dir := w.Engine.kittenDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if _, err := w.ask(ctx, envKitten, map[string]any{"op": "fetch", "repo": KittenRepo, "revision": kittenRevision,
		"dir": dir, "patterns": kittenFiles}); err != nil {
		return fmt.Errorf("couldn't download Kitten's voices: %w", err)
	}
	return os.WriteFile(w.Engine.kittenReadyFile(), []byte(kittenRevision+"\n"), 0o600)
}

// RemoveKitten stops Kitten and deletes its model and environment.
func (w *Worker) RemoveKitten() error {
	j := &w.kitten
	j.mu.Lock()
	cancel, done := j.cancel, j.done
	j.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
	w.stopEnv(envKitten)
	j.mu.Lock()
	j.err = ""
	j.mu.Unlock()
	if err := os.RemoveAll(w.Engine.kittenDir()); err != nil {
		return err
	}
	if r, ok := w.Engine.Python.(interface{ Remove(string) error }); ok {
		return r.Remove(KittenSpec().Name)
	}
	return nil
}

// speakKitten reads text in a Kitten voice.
func (w *Worker) speakKitten(ctx context.Context, text, voice string) ([]byte, int, error) {
	m := KittenVoiceRe.FindStringSubmatch(voice)
	if m == nil {
		return nil, 0, fmt.Errorf("%q is not a Kitten voice", voice)
	}
	key := m[2]
	name := key
	for _, v := range w.Engine.kittenVoices()[m[1]] {
		if v.ID == voice {
			name = v.Name
		}
	}
	a, err := w.ask(ctx, envKitten, map[string]any{"op": "speak", "engine": "kitten", "text": text, "voice": name,
		"lang": m[1], "model": w.Engine.kittenDir()})
	if err != nil {
		return nil, 0, err
	}
	return decodePCM(a)
}

// VoiceChoice is a language and the voices it can be read in, for the
// Voice settings.
type VoiceChoice struct {
	Language string `json:"language"`
	// Voice is the voice an Admin chose, "" for automatic.
	Voice string `json:"voice"`
	// Automatic is the voice used when none is chosen.
	Automatic string        `json:"automatic"`
	Voices    []VoiceOption `json:"voices"`
}

// VoiceOption is one voice to choose from.
type VoiceOption struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Engine is kitten, kokoro, or piper.
	Engine string `json:"engine"`
}

// VoiceChoices are the languages read-aloud speaks and their voices, with
// the voice chosen for each by chosen.
func (w *Worker) VoiceChoices(chosen func(lang string) string) []VoiceChoice {
	langs := map[string]bool{}
	for l := range voices {
		langs[l] = true
	}
	kitten := w.Engine.kittenVoices()
	for l := range kitten {
		langs[l] = true
	}
	out := make([]VoiceChoice, 0, len(langs))
	for l := range langs {
		c := VoiceChoice{Language: l, Automatic: w.automaticVoice(l)}
		if chosen != nil {
			c.Voice = chosen(l)
		}
		for _, v := range kitten[l] {
			c.Voices = append(c.Voices, VoiceOption{ID: v.ID, Name: v.Name, Description: v.Description, Engine: "kitten"})
		}
		if v, ok := naturalVoices[l]; ok && !w.Plain {
			c.Voices = append(c.Voices, VoiceOption{ID: v, Name: kokoroName(v), Engine: "kokoro"})
		}
		if v, ok := voices[l]; ok {
			c.Voices = append(c.Voices, VoiceOption{ID: v, Name: piperName(v), Engine: "piper"})
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Language < out[j].Language })
	return out
}

// ValidVoice says whether voice can be chosen for lang.
func (w *Worker) ValidVoice(lang, voice string) bool {
	for _, c := range w.VoiceChoices(nil) {
		if c.Language != lang {
			continue
		}
		for _, v := range c.Voices {
			if v.ID == voice {
				return true
			}
		}
	}
	return false
}

// automaticVoice is the voice lang is read in when none is chosen: Kokoro
// where it speaks the language, then Kitten when it's installed, then
// Piper.
func (w *Worker) automaticVoice(lang string) string {
	if v, ok := NaturalVoiceFor(lang); ok && !w.Plain {
		return v
	}
	if v := w.Engine.kittenSpeaks(lang); v != "" {
		return v
	}
	v, _ := VoiceFor(lang)
	return v
}

// kokoroName is a Kokoro voice's name: af_heart is Heart.
func kokoroName(v string) string {
	_, n, _ := strings.Cut(v, "_")
	return titleWord(n)
}

// piperName is a Piper voice's name: de_DE-thorsten-medium is Thorsten.
func piperName(v string) string {
	parts := strings.Split(v, "-")
	if len(parts) < 2 {
		return v
	}
	return titleWord(strings.ReplaceAll(parts[1], "_", " "))
}

func titleWord(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// VoiceSettings are the Voice settings: Kitten's install and each
// language's voice.
type VoiceSettings struct {
	Kitten    KittenStatus  `json:"kitten"`
	Languages []VoiceChoice `json:"languages"`
}

// Settings is the Voice settings, with the voices chosen by chosen.
func (w *Worker) Settings(chosen func(lang string) string) VoiceSettings {
	return VoiceSettings{Kitten: w.KittenStatus(), Languages: w.VoiceChoices(chosen)}
}
