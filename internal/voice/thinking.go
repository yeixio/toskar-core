package voice

import (
	"math"
	"time"
)

// The thinking sound (#1334): when an answer takes a while, four soft
// rising bubbles say Toskar heard and is working on it, instead of silence.
// They play only after ThinkingDelay, so a quick answer never has them, and
// again every ThinkingGap, never louder or faster, until the answer is
// ready to speak.
const (
	ThinkingDelay = 1500 * time.Millisecond
	ThinkingGap   = 3000 * time.Millisecond
)

// thinkingPeak is how loud the bubbles are, of full scale: well below
// speech, and below a voice detector's threshold once the room has taken
// its share.
const thinkingPeak = 0.045

// thinkingBubbles are the bubbles: when each starts, the pitch it rises
// from and to, how long it lasts, and how loud it is beside the others.
// Mike chose them from samples on 2026-11-27, after the two-note chime
// proved too bright on the phone. web/src/lib/voice/thinking.ts and the
// phone app (toskar-apps mobile/src/liveVoice.ts) make the same sound.
var thinkingBubbles = []struct{ at, from, to, length, gain float64 }{
	{0, 420, 760, 0.09, 0.6},
	{0.12, 520, 940, 0.08, 0.5},
	{0.25, 470, 850, 0.09, 0.55},
	{0.37, 600, 1050, 0.07, 0.4},
}

// ThinkingSound is the bubbles at rate, half a second in all. Each rises
// in pitch as a bubble does, with a 4 ms start and a quick fade. It is made
// here rather than shipped as a recording, so it plays at any rate as it is.
func ThinkingSound(rate int) []int16 {
	if rate <= 0 {
		return nil
	}
	r := float64(rate)
	n := int(0.5 * r)
	x := make([]float64, n)
	for _, b := range thinkingBubbles {
		start := int(b.at * r)
		length := int(b.length * r)
		phase := 0.0
		for i := 0; i < length && start+i < n; i++ {
			t := float64(i) / r
			freq := b.from * math.Pow(b.to/b.from, t/b.length)
			phase += 2 * math.Pi * freq / r
			env := math.Min(1, t/0.004) * math.Exp(-t/(b.length*0.35))
			x[start+i] += b.gain * env * math.Sin(phase)
		}
	}
	peak := 0.0
	for _, v := range x {
		peak = math.Max(peak, math.Abs(v))
	}
	// The last 10 ms fade to silence, so it ends without a click.
	tail := int(0.01 * r)
	out := make([]int16, n)
	for i, v := range x {
		if left := n - 1 - i; left < tail {
			v *= float64(left) / float64(tail)
		}
		out[i] = int16(math.Round(v / peak * thinkingPeak * 32767))
	}
	return out
}
