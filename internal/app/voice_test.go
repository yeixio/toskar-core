package app

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yeixio/toskar-core/internal/speech"
	"github.com/yeixio/toskar-core/internal/voice"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
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

// A voice chat keeps both sides of every turn, like a written one: the
// answer is saved after it's spoken, and the next turn hears it (#795).
func TestVoiceChatSavesAnswers(t *testing.T) {
	t.Setenv("TOSKAR_STUB_INFERENCE", "1")
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	a, err := New(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })
	var heard atomic.Int32
	a.StubReply = func(_ string, msgs []pluginapi.ChatMessage) string {
		for _, m := range msgs {
			if m.Role == "assistant" && strings.Contains(m.Content, "Otters hold hands") {
				heard.Add(1)
			}
		}
		return "Otters hold hands while they sleep."
	}
	ctx := context.Background()
	chat := &voiceChat{app: a, ctx: ctx}
	for range 2 {
		var said strings.Builder
		if err := chat.Respond(ctx, "Say that again, please.", func(s string) { said.WriteString(s) }); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(said.String(), "Otters hold hands") {
			t.Fatalf("said %q", said.String())
		}
	}
	<-chat.saving
	msgs, err := a.Conversations.ListMessages(ctx, chat.conversationID)
	if err != nil {
		t.Fatal(err)
	}
	answers := 0
	for _, m := range msgs {
		if m.Role == "assistant" {
			answers++
		}
	}
	if answers != 2 {
		t.Fatalf("%d answers saved, want 2", answers)
	}
	if heard.Load() == 0 {
		t.Error("the second turn didn't hear the first answer")
	}
}
