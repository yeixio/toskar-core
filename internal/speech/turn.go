package speech

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Smart Turn v3 (toskar-core#1403): a small model that hears from the audio
// whether a person has finished what they were saying, so a pause mid-thought
// isn't taken for the end of the turn. It's a Whisper Tiny encoder with a
// classifier, 8.7 MB quantized for the processor, about 10 to 100 ms a turn,
// in 23 languages, under the BSD-2-Clause license (pipecat-ai/smart-turn).
// It runs in the speech environment with what Whisper already brings
// (onnxruntime, numpy), so it adds no program, only its file.
const (
	SmartTurnRepo     = "pipecat-ai/smart-turn-v3"
	smartTurnRevision = "f766f81d3cfdf7737ac64aad813d91bbfd56bf93" // v3.2
	smartTurnFile     = "smart-turn-v3.2-cpu.onnx"
	SmartTurnLicense  = "BSD-2-Clause"
)

// ErrTurnNotReady means Smart Turn's file is still downloading: the pause
// alone ends the turn meanwhile.
var ErrTurnNotReady = errors.New("the turn check is still downloading")

// turnJob is Smart Turn's download, done once in the background.
type turnJob struct {
	mu     sync.Mutex
	active bool
	// failedAt is when the last download failed; it's tried again later.
	failedAt time.Time
}

func (e *Engine) smartTurnPath() string {
	return filepath.Join(e.Dir, "smart-turn", smartTurnFile)
}

// PrepareTurnCheck downloads Smart Turn's file in the background, if it
// isn't here yet.
func (w *Worker) PrepareTurnCheck() {
	if w.Engine == nil {
		return
	}
	if _, err := os.Stat(w.Engine.smartTurnPath()); err == nil {
		return
	}
	j := &w.turn
	j.mu.Lock()
	if j.active || time.Since(j.failedAt) < 10*time.Minute {
		j.mu.Unlock()
		return
	}
	j.active = true
	j.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		_, err := w.ask(ctx, envSpeech, map[string]any{"op": "fetch", "repo": SmartTurnRepo, "revision": smartTurnRevision,
			"dir": filepath.Dir(w.Engine.smartTurnPath()), "patterns": []string{smartTurnFile}})
		j.mu.Lock()
		j.active = false
		if err != nil {
			j.failedAt = time.Now()
		}
		j.mu.Unlock()
	}()
}

// TurnFinished is how likely it is, from 0 to 1, that the person has
// finished speaking at the end of pcm, 16-bit mono at 16 kHz. Until its
// file is here it answers ErrTurnNotReady, and starts the download.
func (w *Worker) TurnFinished(ctx context.Context, pcm []byte) (float64, error) {
	if w.Engine == nil {
		return 0, ErrTurnNotReady
	}
	path := w.Engine.smartTurnPath()
	if _, err := os.Stat(path); err != nil {
		w.PrepareTurnCheck()
		return 0, ErrTurnNotReady
	}
	a, err := w.ask(ctx, envSpeech, map[string]any{"op": "turn", "model": path, "pcm": base64.StdEncoding.EncodeToString(pcm)})
	if err != nil {
		return 0, err
	}
	return a.Finished, nil
}
