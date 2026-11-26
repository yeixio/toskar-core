package speech

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
)

// What speech downloads the first time it needs it, told to
// Engine.Fetching so What left this computer lists it (#1415): a Whisper
// model, a Piper voice, Kokoro's files, and Kitten's voices. The Python
// environment itself is recorded when pyenv installs it.

// fetching tells Fetching that a download is about to start.
func (e *Engine) fetching(ctx context.Context, to, what string) {
	if e != nil && e.Fetching != nil {
		e.Fetching(context.WithoutCancel(ctx), to, what)
	}
}

// whisperMissing is whether a Whisper model hasn't been downloaded:
// faster-whisper keeps it as models--Systran--faster-whisper-<name>.
func (e *Engine) whisperMissing(model string) bool {
	found, _ := filepath.Glob(filepath.Join(e.Dir, "whisper", "models--*--faster-whisper-"+model))
	return len(found) == 0
}

// beforeWhisper records the Whisper model's download when it isn't here.
func (e *Engine) beforeWhisper(ctx context.Context, model string) {
	if e.whisperMissing(model) {
		e.fetching(ctx, "Hugging Face Hub", "Whisper "+model+" speech recognition model, the first time it's used")
	}
}

// beforeVoice records a Piper voice's download when it isn't here.
func (e *Engine) beforeVoice(ctx context.Context, voice string) {
	if _, err := os.Stat(filepath.Join(e.Dir, "voices", voice+".onnx")); err != nil {
		e.fetching(ctx, "Hugging Face Hub", "Piper voice "+voice+", the first time it's used")
	}
}

// hostOf is a download address's host, or the address when it has none.
func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return raw
}
