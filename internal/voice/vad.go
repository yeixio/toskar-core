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
	return &TurnDetection{Type: "server_vad", Threshold: 0.5, PrefixPaddingMs: 300, SilenceDurationMs: 500, CreateResponse: &yes, InterruptResponse: &yes}
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
	loud     int // speech frames in a row
	quiet    int // quiet frames in a row while speaking
	pending  []int16
}

// startFrames of speech in a row start a turn: 60 ms, so a click doesn't.
const startFrames = 3

func newVAD(cfg TurnDetection) *vad {
	if cfg.Threshold <= 0 || cfg.Threshold > 1 {
		cfg.Threshold = 0.5
	}
	if cfg.SilenceDurationMs <= 0 {
		cfg.SilenceDurationMs = 500
	}
	if cfg.PrefixPaddingMs < 0 {
		cfg.PrefixPaddingMs = 0
	}
	return &vad{cfg: cfg, noise: 0.002}
}

// floor is the least level that counts as speech.
func (v *vad) floor() float64 {
	min := 0.04 * v.cfg.Threshold // 0.02, about -34 dBFS, at 0.5
	if n := v.noise * 4; n > min {
		return n
	}
	return min
}

// vadEvent is what a frame changed: started or stopped speaking, with the
// sample the change happened at, counted from the start of the session.
type vadEvent struct {
	started, stopped bool
	at               int
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
		loud := lvl >= v.floor()
		if !v.speaking {
			if loud {
				v.loud++
			} else {
				v.loud = 0
				// The room's noise, slowly, while nobody speaks.
				v.noise = 0.95*v.noise + 0.05*lvl
			}
			if v.loud >= startFrames {
				v.speaking, v.quiet = true, 0
				out = append(out, vadEvent{started: true, at: at - (startFrames-1)*frameSamples})
			}
			continue
		}
		if loud {
			v.quiet = 0
			continue
		}
		v.quiet++
		if v.quiet*20 >= v.cfg.SilenceDurationMs {
			v.speaking, v.loud = false, 0
			out = append(out, vadEvent{stopped: true, at: at + frameSamples - v.quiet*frameSamples})
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
	return &Turns{v: newVAD(cfg)}
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
