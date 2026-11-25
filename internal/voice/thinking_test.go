package voice

import (
	"math"
	"testing"
)

// The thinking sound is short and soft, and never starts a turn when the
// microphone hears it (#1334).
func TestThinkingSound(t *testing.T) {
	s := ThinkingSound(SampleRate)
	if d := float64(len(s)) / SampleRate; d < 0.4 || d > 0.8 {
		t.Fatalf("%.2f s long", d)
	}
	peak := 0.0
	for _, v := range s {
		peak = math.Max(peak, math.Abs(float64(v))/32767)
	}
	if peak > thinkingPeak+0.001 || peak < thinkingPeak/2 {
		t.Fatalf("peak %.3f", peak)
	}
	if s[0] != 0 || s[len(s)-1] != 0 {
		t.Fatalf("starts at %d, ends at %d: it would click", s[0], s[len(s)-1])
	}
	// Heard back at its full level, looping as it does, by voice chat's
	// detector: it is never taken for someone talking, since it is too short
	// and fades too fast for a word. The door doesn't listen while it plays
	// anything, so its more sensitive detector never hears it.
	echo := make([]int16, 0, len(s)*4)
	for i := 0; i < 4; i++ {
		echo = append(echo, s...)
		echo = append(echo, make([]int16, SampleRate/2)...)
	}
	turns := NewTurns(TurnDetection{})
	for i := 0; i+480 <= len(echo); i += 480 {
		if started, _ := turns.Feed(echo[i : i+480]); started {
			t.Fatalf("the chime was heard as someone talking, at %.2f s", float64(i)/SampleRate)
		}
	}
	if ThinkingSound(0) != nil {
		t.Fatal("a sound at no rate")
	}
}
