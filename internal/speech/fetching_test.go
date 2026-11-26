package speech

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// A model or voice is recorded only when it's about to be downloaded (#1415).
func TestFetchingRecordsOnlyWhatIsMissing(t *testing.T) {
	dir := t.TempDir()
	var got []string
	e := &Engine{Dir: dir, Fetching: func(_ context.Context, to, what string) { got = append(got, to+": "+what) }}
	ctx := context.Background()
	e.beforeWhisper(ctx, "base")
	e.beforeVoice(ctx, DefaultVoice)
	if len(got) != 2 {
		t.Fatalf("missing model and voice: %v", got)
	}
	_ = os.MkdirAll(filepath.Join(dir, "whisper", "models--Systran--faster-whisper-base"), 0o700)
	_ = os.MkdirAll(filepath.Join(dir, "voices"), 0o700)
	_ = os.WriteFile(filepath.Join(dir, "voices", DefaultVoice+".onnx"), []byte("x"), 0o600)
	e.beforeWhisper(ctx, "base")
	e.beforeVoice(ctx, DefaultVoice)
	e.beforeWhisper(ctx, "small")
	if len(got) != 3 || got[2] != "Hugging Face Hub: Whisper small speech recognition model, the first time it's used" {
		t.Fatalf("after downloading: %v", got)
	}
	if hostOf(kokoroRelease) != "github.com" {
		t.Errorf("kokoro host %q", hostOf(kokoroRelease))
	}
	(&Engine{}).fetching(ctx, "x", "y") // no hook: nothing happens
}
