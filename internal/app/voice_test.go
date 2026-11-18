package app

import (
	"context"
	"testing"

	"github.com/yeixio/toskar-core/internal/speech"
	"github.com/yeixio/toskar-core/internal/voice"
)

// Live voice needs speech and its worker on this computer (#795).
func TestVoicePartsNeedSpeech(t *testing.T) {
	a := &App{}
	if _, err := a.voiceParts(context.Background()); err == nil {
		t.Fatal("voice parts without speech")
	}
	a.Speech = &speech.Engine{}
	if _, err := a.voiceParts(context.Background()); err == nil {
		t.Fatal("voice parts without the speech worker")
	}
}

// Whisper hears at 16 kHz; a turn arrives at the protocol's 24 kHz.
func TestWorkerTurnsResampleForWhisper(t *testing.T) {
	samples := make([]int16, voice.SampleRate)
	got := voice.Resample(samples, voice.SampleRate, whisperRate)
	if len(got) != whisperRate {
		t.Fatalf("a second of audio is %d samples at 16 kHz", len(got))
	}
}
