package voice

import (
	"math"
	"time"
)

// The thinking sound (#1334): when an answer takes a while, a soft two-note
// chime says Toskar heard and is working on it, instead of silence. It's
// played only after ThinkingDelay, so a quick answer never has it, and
// again every ThinkingGap, never louder or faster, until the answer is
// ready to speak.
const (
	ThinkingDelay = 1500 * time.Millisecond
	ThinkingGap   = 2500 * time.Millisecond
)

// thinkingPeak is how loud the chime is, of full scale: well below speech,
// and below a voice detector's threshold once the room has taken its share.
const thinkingPeak = 0.12

// ThinkingSound is the chime at rate: two warm notes, a fifth apart, each a
// quick soft start and a bell's fade, about 0.6 s in all. It is made here
// rather than shipped as a recording, so it plays at any rate as it is.
func ThinkingSound(rate int) []int16 {
	if rate <= 0 {
		return nil
	}
	notes := []struct {
		at, freq float64
	}{{0, 659.25}, {0.16, 987.77}} // E5, then B5
	n := int(0.62 * float64(rate))
	x := make([]float64, n)
	for _, note := range notes {
		start := int(note.at * float64(rate))
		for i := start; i < n; i++ {
			t := float64(i-start) / float64(rate)
			// 12 ms to full, then a fade with a 110 ms time constant.
			env := math.Min(1, t/0.012) * math.Exp(-t/0.11)
			// A quiet octave above, for warmth rather than a bare beep.
			w := 2 * math.Pi * note.freq * t
			x[i] += env * (math.Sin(w) + 0.18*math.Sin(2*w))
		}
	}
	peak := 0.0
	for _, v := range x {
		peak = math.Max(peak, math.Abs(v))
	}
	// The last 20 ms fade to silence, so it ends without a click.
	tail := int(0.02 * float64(rate))
	out := make([]int16, n)
	for i, v := range x {
		if left := n - i; left < tail {
			v *= float64(left) / float64(tail)
		}
		out[i] = int16(math.Round(v / peak * thinkingPeak * 32767))
	}
	return out
}
