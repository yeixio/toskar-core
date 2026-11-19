package voicebench

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"

	"github.com/yeixio/toskar-core/internal/voice"
)

// rate is the live voice protocol's: everything here is at 24 kHz.
const rate = voice.SampleRate

// clip is one spoken phrase from clips/clips.json.
type clip struct {
	ID      string `json:"id"`
	Lang    string `json:"lang"`
	Text    string `json:"text"`
	samples []float64
}

// utterance is one turn the person takes: one or more phrases with the
// pauses they take between them. Every pause is mid-sentence, so a good
// listener waits through it and answers once, after the last part.
type utterance struct {
	ID     string
	Lang   string
	Parts  []string
	Pauses []int // ms between parts
}

// utterances are what the person says in each condition, in order: short
// and long turns, a one-word answer, and pauses from 0.5 s, between
// clauses, to 1.4 s, where someone stops to think.
var utterances = []utterance{
	{ID: "weather", Lang: "en", Parts: []string{"en_weather"}},
	{ID: "dentist, 0.5 s pause", Lang: "en", Parts: []string{"en_dentist_1", "en_dentist_2"}, Pauses: []int{500}},
	{ID: "lights", Lang: "en", Parts: []string{"en_lights"}},
	{ID: "shopping list, 0.7 s pause", Lang: "en", Parts: []string{"en_list_1", "en_list_2"}, Pauses: []int{700}},
	{ID: "yes", Lang: "en", Parts: []string{"en_yes"}},
	{ID: "pizza, 1.0 s pause", Lang: "en", Parts: []string{"en_pizza_1", "en_pizza_2"}, Pauses: []int{1000}},
	{ID: "Spanish weather", Lang: "es", Parts: []string{"es_tiempo"}},
	{ID: "Spanish dinner, 0.8 s pause", Lang: "es", Parts: []string{"es_cena_1", "es_cena_2"}, Pauses: []int{800}},
	{ID: "German time", Lang: "de", Parts: []string{"de_tokio"}},
	{ID: "movie, 1.4 s pause", Lang: "en", Parts: []string{"en_movie_1", "en_movie_2"}, Pauses: []int{1400}},
	{ID: "French question", Lang: "fr", Parts: []string{"fr_photo"}},
	{ID: "thanks", Lang: "en", Parts: []string{"en_thanks"}},
}

// condition is how the person's voice reaches the microphone, and what else
// it hears.
type condition struct {
	Name, About string
	// speechDB is the speech's level, in dB of full scale RMS.
	speechDB float64
	// farField adds a room's echo and a webcam microphone's narrow band.
	farField bool
	// bed is the noise under everything, made fresh for each run.
	bed func(r *rand.Rand, clips map[string]*clip) noise
}

var conditions = []condition{
	{Name: "headset", About: "a headset microphone in a quiet room", speechDB: -20,
		bed: func(r *rand.Rand, _ map[string]*clip) noise { return &white{r: r, amp: dbAmp(-75)} }},
	// The browser's automatic gain brings a far voice up, and the room
	// with it: quieter than a headset, with the room's echo and sound
	// about 20 dB under the voice.
	{Name: "webcam", About: "a webcam microphone across a room with ordinary, not loud, room sound", speechDB: -28, farField: true,
		bed: func(r *rand.Rand, _ map[string]*clip) noise {
			return sum{&pink{r: r, amp: dbAmp(-50)}, &white{r: r, amp: dbAmp(-58)}}
		}},
	{Name: "hiss", About: "a microphone with steady white noise (hiss)", speechDB: -24,
		bed: func(r *rand.Rand, _ map[string]*clip) noise { return &white{r: r, amp: dbAmp(-44)} }},
	{Name: "fan", About: "a fan or air conditioner running nearby", speechDB: -24,
		bed: func(r *rand.Rand, _ map[string]*clip) noise { return &fan{r: r, amp: dbAmp(-40)} }},
	{Name: "tv", About: "a TV or other people talking in the background", speechDB: -24,
		bed: func(r *rand.Rand, clips map[string]*clip) noise { return newBabble(r, clips, dbAmp(-38)) }},
}

// loadClips reads clips/clips.json and each clip's WAV, at rate.
func loadClips(dir string) (map[string]*clip, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "clips.json"))
	if err != nil {
		return nil, err
	}
	var list struct {
		Clips []*clip `json:"clips"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	out := map[string]*clip{}
	for _, c := range list.Clips {
		b, err := os.ReadFile(filepath.Join(dir, c.ID+".wav"))
		if err != nil {
			return nil, err
		}
		s, r, err := voice.ParseWAV(b)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", c.ID, err)
		}
		c.samples = toFloat(voice.Resample(s, r, rate))
		out[c.ID] = c
	}
	return out, nil
}

// text is what the utterance says, all its parts.
func (u utterance) text(clips map[string]*clip) string {
	s := ""
	for i, p := range u.Parts {
		if i > 0 {
			s += " "
		}
		s += clips[p].Text
	}
	return s
}

// spoken is an utterance rendered for a condition: its audio, starting at
// sample 0, and where the speech in it is.
type spoken struct {
	audio []float64 // speech only, before the noise bed; a far-field echo rings past end
	// start and end are the first and last samples of speech; active is
	// each 20 ms frame's speech, for what's heard of it.
	start, end int
	active     []bool
}

const frame = rate / 50

// render lays out an utterance's parts with their pauses at the
// condition's level, through the room when it's far away.
func render(u utterance, c condition, clips map[string]*clip, r *rand.Rand) spoken {
	var dry []float64
	for i, p := range u.Parts {
		if i > 0 {
			dry = append(dry, make([]float64, u.Pauses[i-1]*rate/1000)...)
		}
		dry = append(dry, clips[p].samples...)
	}
	// Level by the speech's RMS over its voiced frames.
	gain := dbAmp(c.speechDB) / activeRMS(dry)
	for i := range dry {
		dry[i] *= gain
	}
	active := make([]bool, (len(dry)+frame-1)/frame)
	for f := range active {
		active[f] = rms(dry[f*frame:min(len(dry), (f+1)*frame)]) > dbAmp(c.speechDB-25)
	}
	out := dry
	if c.farField {
		out = room(dry, r)
	}
	return spoken{audio: out, start: 0, end: len(dry), active: active}
}

// room is a talker a few meters from a webcam: the room's echo (about
// 0.5 s RT60, as much echo as direct sound) and a small microphone's band,
// about 150 Hz to 6 kHz.
func room(dry []float64, r *rand.Rand) []float64 {
	const taps = 600
	const rt60 = 0.5
	tail := int(rt60 * rate)
	type tap struct {
		at int
		g  float64
	}
	ts := []tap{{0, 1}}
	var energy float64
	for i := 0; i < taps; i++ {
		at := 120 + r.IntN(tail) // first reflections after 5 ms
		g := (r.Float64()*2 - 1) * math.Pow(10, -3*float64(at)/float64(tail))
		ts = append(ts, tap{at, g})
		energy += g * g
	}
	// As much echo as direct sound.
	scale := 1 / math.Sqrt(energy)
	for i := 1; i < len(ts); i++ {
		ts[i].g *= scale
	}
	out := make([]float64, len(dry)+tail+120)
	for i, x := range dry {
		if x == 0 {
			continue
		}
		for _, t := range ts {
			out[i+t.at] += x * t.g
		}
	}
	return band(out, 150, 6000)
}

// band filters with one-pole high- and low-pass filters.
func band(x []float64, lo, hi float64) []float64 {
	out := make([]float64, len(x))
	hpA := 1 / (1 + 2*math.Pi*lo/rate)
	lpA := 2 * math.Pi * hi / rate / (1 + 2*math.Pi*hi/rate)
	var prevX, hp, lp float64
	for i, v := range x {
		hp = hpA * (hp + v - prevX)
		prevX = v
		lp += lpA * (hp - lp)
		out[i] = lp
	}
	// The filters take a little level; keep the speech's.
	if g := activeRMS(x) / activeRMS(out); g > 0 && !math.IsInf(g, 0) {
		for i := range out {
			out[i] *= g
		}
	}
	return out
}

// noise is a steady sound, made as it's needed.
type noise interface{ next(n int) []float64 }

type white struct {
	r   *rand.Rand
	amp float64
}

func (w *white) next(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = w.r.NormFloat64() * w.amp
	}
	return out
}

// pink is a room's ambient sound: more low than high (Paul Kellet's filter).
type pink struct {
	r          *rand.Rand
	amp        float64
	b0, b1, b2 float64
}

func (p *pink) next(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		w := p.r.NormFloat64()
		p.b0 = 0.99765*p.b0 + w*0.0990460
		p.b1 = 0.96300*p.b1 + w*0.2965164
		p.b2 = 0.57000*p.b2 + w*1.0526913
		out[i] = (p.b0 + p.b1 + p.b2 + w*0.1848) * p.amp / 3.2
	}
	return out
}

// fan is broadband rumble with the motor's hum at 120 and 240 Hz.
type fan struct {
	r   *rand.Rand
	amp float64
	lp  float64
	t   int
}

func (f *fan) next(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		f.lp += 0.08 * (f.r.NormFloat64() - f.lp)
		ph := 2 * math.Pi * float64(f.t) / rate
		hum := 0.25*math.Sin(120*ph) + 0.12*math.Sin(240*ph)
		out[i] = (f.lp*3.2 + hum) * f.amp
		f.t++
	}
	return out
}

// babble is other people talking: the clips backwards, so it sounds like
// speech but has no words to transcribe, overlapping at random.
type babble struct {
	r      *rand.Rand
	amp    float64
	voices [][]float64
	buf    []float64
}

func newBabble(r *rand.Rand, clips map[string]*clip, amp float64) *babble {
	ids := make([]string, 0, len(clips))
	for id := range clips {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var voices [][]float64
	for _, id := range ids {
		s := clips[id].samples
		rev := make([]float64, len(s))
		for i := range s {
			rev[i] = s[len(s)-1-i]
		}
		g := 1 / activeRMS(rev)
		for i := range rev {
			rev[i] *= g
		}
		voices = append(voices, rev)
	}
	return &babble{r: r, amp: amp, voices: voices}
}

func (b *babble) next(n int) []float64 {
	for len(b.buf) < n+rate {
		// Two talkers at once, with short gaps.
		a := b.voices[b.r.IntN(len(b.voices))]
		c := b.voices[b.r.IntN(len(b.voices))]
		seg := make([]float64, max(len(a), len(c)+rate/3)+rate/5)
		for i, v := range a {
			seg[i] += v
		}
		for i, v := range c {
			seg[i+rate/3] += 0.7 * v
		}
		b.buf = append(b.buf, seg...)
	}
	out := make([]float64, n)
	for i := range out {
		out[i] = b.buf[i] * b.amp / 1.2
	}
	b.buf = b.buf[n:]
	return out
}

type sum []noise

func (s sum) next(n int) []float64 {
	out := make([]float64, n)
	for _, src := range s {
		for i, v := range src.next(n) {
			out[i] += v
		}
	}
	return out
}

// noiseScene is sounds with nobody talking, in a quiet room: none of it is
// a turn. Each is at second at, for its length.
type noiseEvent struct {
	Name     string
	At, Secs float64
	make     func(r *rand.Rand, n int, clips map[string]*clip) []float64
}

var noiseEvents = []noiseEvent{
	{Name: "a knock, three raps", At: 2, Secs: 0.7, make: func(r *rand.Rand, n int, _ map[string]*clip) []float64 {
		out := make([]float64, n)
		for k := 0; k < 3; k++ {
			at := k * rate / 4
			for i := 0; i < rate/50 && at+i < n; i++ {
				out[at+i] = r.NormFloat64() * dbAmp(-14) * math.Exp(-float64(i)/(rate/400))
			}
		}
		return out
	}},
	{Name: "a cough", At: 5, Secs: 0.25, make: func(r *rand.Rand, n int, _ map[string]*clip) []float64 {
		out := make([]float64, n)
		var lp float64
		for i := range out {
			lp += 0.3 * (r.NormFloat64() - lp)
			env := math.Sin(math.Pi * float64(i) / float64(n))
			out[i] = lp * dbAmp(-18) * 2 * env
		}
		return out
	}},
	{Name: "a fan switched on", At: 8, Secs: 6, make: func(r *rand.Rand, n int, _ map[string]*clip) []float64 {
		return (&fan{r: r, amp: dbAmp(-38)}).next(n)
	}},
	{Name: "steady hiss starting", At: 16, Secs: 6, make: func(r *rand.Rand, n int, _ map[string]*clip) []float64 {
		return (&white{r: r, amp: dbAmp(-42)}).next(n)
	}},
	{Name: "a TV starting", At: 24, Secs: 6, make: func(r *rand.Rand, n int, clips map[string]*clip) []float64 {
		return newBabble(r, clips, dbAmp(-40)).next(n)
	}},
}

// noiseSceneSecs is how long the noise scene runs.
const noiseSceneSecs = 32

// noiseScene renders noiseEvents over a quiet room, with where each is.
func noiseScene(r *rand.Rand, clips map[string]*clip) []float64 {
	out := (&white{r: r, amp: dbAmp(-75)}).next(noiseSceneSecs * rate)
	for _, e := range noiseEvents {
		at := int(e.At * rate)
		n := int(e.Secs * rate)
		for i, v := range e.make(r, n, clips) {
			if at+i < len(out) {
				out[at+i] += v
			}
		}
	}
	return out
}

func dbAmp(db float64) float64 { return math.Pow(10, db/20) }

func rms(x []float64) float64 {
	if len(x) == 0 {
		return 0
	}
	var s float64
	for _, v := range x {
		s += v * v
	}
	return math.Sqrt(s / float64(len(x)))
}

// activeRMS is the RMS of a recording's loudest frames, the speech, not
// the pauses.
func activeRMS(x []float64) float64 {
	var levels []float64
	for f := 0; f*frame < len(x); f++ {
		levels = append(levels, rms(x[f*frame:min(len(x), (f+1)*frame)]))
	}
	if len(levels) == 0 {
		return 1
	}
	peak := 0.0
	for _, l := range levels {
		peak = math.Max(peak, l)
	}
	var s float64
	var n int
	for _, l := range levels {
		if l > peak/10 {
			s += l * l
			n++
		}
	}
	return math.Sqrt(s / float64(n))
}

func toFloat(s []int16) []float64 {
	out := make([]float64, len(s))
	for i, v := range s {
		out[i] = float64(v) / 32768
	}
	return out
}

func toPCM(x []float64) []int16 {
	out := make([]int16, len(x))
	for i, v := range x {
		out[i] = int16(math.Max(-32768, math.Min(32767, math.Round(v*32768))))
	}
	return out
}
