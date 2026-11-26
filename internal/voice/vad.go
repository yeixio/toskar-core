package voice

// The server's turn detection (OpenAI's server_vad): finds where the person
// starts and stops talking, from how loud each 20 ms frame is against the
// room's own noise. It's the first step of #795's turn-taking; a model that
// judges whether a turn sounds finished (Smart Turn) can come after it.

const frameSamples = SampleRate / 50 // 20 ms

// TurnDetection is session.turn_detection.
type TurnDetection struct {
	// Type is server_vad, or "" for none: the client commits each turn.
	Type string `json:"type"`
	// Threshold is how loud speech must be, 0 to 1; 0.5 by default. Higher
	// for a noisy room.
	Threshold float64 `json:"threshold,omitempty"`
	// PrefixPaddingMs is the audio kept from before speech started.
	PrefixPaddingMs int `json:"prefix_padding_ms,omitempty"`
	// SilenceDurationMs is the quiet that ends a turn.
	SilenceDurationMs int `json:"silence_duration_ms,omitempty"`
	// CreateResponse answers each turn; false leaves it to response.create.
	CreateResponse *bool `json:"create_response,omitempty"`
	// InterruptResponse stops the answer when the person starts talking.
	InterruptResponse *bool `json:"interrupt_response,omitempty"`
}

func defaultTurnDetection() *TurnDetection {
	yes := true
	return &TurnDetection{Type: "server_vad", Threshold: 0.5, PrefixPaddingMs: 300, SilenceDurationMs: 900, CreateResponse: &yes, InterruptResponse: &yes}
}

func (t *TurnDetection) creates() bool {
	return t == nil || t.CreateResponse == nil || *t.CreateResponse
}
func (t *TurnDetection) interrupts() bool {
	return t == nil || t.InterruptResponse == nil || *t.InterruptResponse
}

// vad tracks one session's audio.
type vad struct {
	cfg TurnDetection
	// noise is the room's level while nobody speaks.
	noise    float64
	speaking bool
	loud     int // speech frames lately, less two for each quiet one
	span     int // frames since they started
	quiet    int // quiet frames in a row while speaking
	frames   int // frames heard
	pending  []int16
	// recent are the levels of the turn's last pauseFrames frames, from
	// next, while someone speaks.
	recent [pauseFrames]float64
	next   int
	heard  int // frames in recent
	// pause counts the pauses that reached earlyPauseMs, so a judgment
	// knows whether its pause is still the one going on (#1460).
	pause int
}

// earlyPauseMs is the quiet after which a paused turn is judged, while the
// pause goes on (#1460). Smart Turn is made for about this much silence
// after the words, and a finished verdict then ends the turn sooner than
// the whole pause would have.
const earlyPauseMs = 300

// inPause reports that the turn is still paused at pause.
func (v *vad) inPause(pause int) bool { return v.speaking && v.quiet > 0 && v.pause == pause }

// end ends the turn now, as the pause would have.
func (v *vad) end() { v.speaking, v.loud, v.quiet = false, 0, 0 }

// pauseFrames are 2.5 s: speech pauses, if only to breathe, at least that
// often, and its pauses fall to the room's noise; a fan or traffic that
// started during a turn doesn't. So the quietest of a turn's last 2.5 s is
// at most the room's noise, and when it's louder than what was learned,
// the room got louder (#795).
const pauseFrames = 125

// warmFrames are the first 300 ms, which learn the room's noise before any
// turn can start: a fan or a hum that's there from the start isn't speech.
const warmFrames = 15

// startFrames of speech start a turn: 160 ms, so a click, a cough, or a
// chair doesn't, and doesn't stop an answer (#795). The prefix padding
// keeps the start of the first word.
const startFrames = 8

func newVAD(cfg TurnDetection) *vad {
	if cfg.Threshold <= 0 || cfg.Threshold > 1 {
		cfg.Threshold = 0.5
	}
	if cfg.SilenceDurationMs <= 0 {
		cfg.SilenceDurationMs = 900
	}
	if cfg.PrefixPaddingMs < 0 {
		cfg.PrefixPaddingMs = 0
	}
	return &vad{cfg: cfg, noise: 0.002}
}

// floor is the least level that counts as speech.
func (v *vad) floor() float64 {
	min := 0.06 * v.cfg.Threshold // 0.03, about -30 dBFS, at 0.5
	if n := v.noise * 4; n > min {
		return n
	}
	return min
}

// keepFloor is the least level that keeps a turn going, once it started:
// lower than the floor that starts one, so a word that trails off, or a
// softer one at the end, from a microphone across the room isn't heard as
// the quiet that ends the turn (#795).
func (v *vad) keepFloor() float64 { return keepRatio * v.floor() }

// keepRatio is how much quieter than the start the words may get.
const keepRatio = 0.6

// learnNoise follows the room's noise (#795): down quickly, when it gets
// quieter, and up slowly, over about two seconds from what's below the
// floor and ten from what's above it, so a fan or traffic that starts
// becomes the room rather than a turn that never ends, while speech isn't
// learned as noise.
// It used to learn only from quiet frames, so a steady noise louder than
// the floor was never learned, and kept starting turns.
//
// While someone speaks, it rises only to the quietest of the last 2.5 s:
// rising with the speech, a long sentence from across the room raised the
// floor above its own last words, and the turn ended before it did.
func (v *vad) learnNoise(lvl float64) {
	if v.speaking {
		v.recent[v.next] = lvl
		v.next = (v.next + 1) % pauseFrames
		v.heard = min(v.heard+1, pauseFrames)
		if lvl < v.noise {
			v.noise += 0.1 * (lvl - v.noise)
		} else if v.heard == pauseFrames {
			least := v.recent[0]
			for _, l := range v.recent[1:] {
				least = min(least, l)
			}
			v.noise = max(v.noise, least)
		}
		return
	}
	v.heard, v.next = 0, 0
	switch {
	case lvl < v.noise:
		v.noise += 0.1 * (lvl - v.noise)
	case lvl >= v.floor():
		v.noise += 0.002 * (lvl - v.noise)
	default:
		v.noise += 0.01 * (lvl - v.noise)
	}
}

// vadEvent is what a frame changed: started or stopped speaking, with the
// sample the change happened at, counted from the start of the session.
type vadEvent struct {
	started, stopped bool
	// paused is earlyPauseMs of quiet in a turn, before it stops; pause
	// names it, and at is where the quiet began.
	paused bool
	pause  int
	at     int
}

// feed takes samples, given at offset in the session's audio, and reports
// what changed.
func (v *vad) feed(samples []int16, offset int) []vadEvent {
	var out []vadEvent
	v.pending = append(v.pending, samples...)
	start := offset + len(samples) - len(v.pending)
	for len(v.pending) >= frameSamples {
		frame := v.pending[:frameSamples]
		v.pending = v.pending[frameSamples:]
		at := start
		start += frameSamples
		lvl := level(frame)
		v.frames++
		if v.frames <= warmFrames {
			v.noise += (lvl - v.noise) / float64(v.frames)
			continue
		}
		loud := lvl >= v.floor()
		if v.speaking {
			loud = lvl >= v.keepFloor()
		}
		v.learnNoise(lvl)
		if !v.speaking {
			if loud {
				v.loud++
				v.span++
			} else if v.loud > 0 {
				// A dip between syllables doesn't start over, but a click
				// followed by quiet fades out.
				v.loud = max(0, v.loud-2)
				v.span++
				if v.loud == 0 {
					v.span = 0
				}
			}
			if v.loud >= startFrames {
				v.speaking, v.quiet = true, 0
				out = append(out, vadEvent{started: true, at: at - (v.span-1)*frameSamples})
				v.span = 0
			}
			continue
		}
		if loud {
			v.quiet = 0
			continue
		}
		v.quiet++
		if v.quiet*20 == earlyPauseMs && earlyPauseMs < v.cfg.SilenceDurationMs {
			v.pause++
			out = append(out, vadEvent{paused: true, pause: v.pause, at: at + frameSamples - v.quiet*frameSamples})
		}
		if v.quiet*20 >= v.cfg.SilenceDurationMs {
			v.speaking, v.loud = false, 0
			out = append(out, vadEvent{stopped: true, pause: v.pause, at: at + frameSamples - v.quiet*frameSamples})
		}
	}
	return out
}

// Turns finds where someone starts and stops talking in a stream of audio
// at SampleRate, as a session's turn detection does: the doorbell uses it
// to hear when a visitor has finished (#667).
type Turns struct {
	v  *vad
	at int
}

// NewTurns detects turns as cfg says; a zero cfg is the session's default.
func NewTurns(cfg TurnDetection) *Turns {
	if cfg.Type == "" {
		cfg = *defaultTurnDetection()
	}
	t := &Turns{v: newVAD(cfg)}
	// It's given the audio from where a turn may start, such as just after
	// the doorbell spoke, so it learns the noise as it goes instead.
	t.v.frames = warmFrames
	return t
}

// Feed takes the next samples and reports whether speech started, and
// whether it stopped, in them.
func (t *Turns) Feed(samples []int16) (started, stopped bool) {
	for _, e := range t.v.feed(samples, t.at) {
		started = started || e.started
		stopped = stopped || e.stopped
	}
	t.at += len(samples)
	return started, stopped
}

// Speaking reports someone talking now.
func (t *Turns) Speaking() bool { return t.v.speaking }
