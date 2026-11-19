package voicebench

import (
	"math"
	"math/rand/v2"
	"sort"
	"strings"
	"time"
)

// event is a server event, with where in the audio sent so far it arrived:
// at is the sample count streamed when it came.
type event struct {
	Type   string
	at     int
	fields map[string]any
}

// link is a live voice session: in this process, or a Toskar's over its
// WebSocket.
type link interface {
	// send streams the next samples.
	send(pcm []int16)
	// events are all the session's events so far.
	events() []event
	// busy is whether the person is mid-turn, or a turn is still being
	// transcribed or answered.
	busy() bool
	close()
}

// truth is where an utterance is in a run's audio.
type truth struct {
	u          utterance
	text       string
	start, end int    // samples
	active     []bool // 20 ms frames of speech, from start
}

// run is one condition's conversation: what was said, and what came back.
type run struct {
	cond    string
	said    []truth
	events  []event
	samples int
}

// converse streams a condition's conversation over l: quiet, then each
// utterance with the room's noise under it, waiting for each to be heard
// and answered.
func converse(l link, c condition, clips map[string]*clip, seed uint64, gap time.Duration) run {
	r := rand.New(rand.NewPCG(seed, 0x70ce))
	bed := c.bed(r, clips)
	out := run{cond: c.Name}
	pos := 0
	stream := func(overlay []float64) {
		x := bed.next(len(overlay))
		for i := range x {
			x[i] += overlay[i]
		}
		for i := 0; i < len(x); i += frame {
			l.send(toPCM(x[i:min(len(x), i+frame)]))
		}
		pos += len(x)
	}
	quiet := func(secs float64) { stream(make([]float64, int(secs*rate))) }
	quiet(1.5)
	for _, u := range utterances {
		sp := render(u, c, clips, r)
		out.said = append(out.said, truth{u: u, text: u.text(clips), start: pos + sp.start, end: pos + sp.end, active: sp.active})
		stream(sp.audio)
		// Wait as a person does: at least gap, longer while Toskar is
		// still hearing or answering, up to 30 s.
		waited := 0
		for waited < int(gap.Seconds()*rate) || (l.busy() && waited < 30*rate) {
			quiet(0.1)
			waited += rate / 10
		}
	}
	quiet(1)
	out.events, out.samples = l.events(), pos
	return out
}

// listenNoise streams the noise scene: nobody talks.
func listenNoise(l link, clips map[string]*clip, seed uint64) run {
	r := rand.New(rand.NewPCG(seed, 0x0153))
	x := noiseScene(r, clips)
	for i := 0; i < len(x); i += frame {
		l.send(toPCM(x[i:min(len(x), i+frame)]))
	}
	for waited := 0; l.busy() && waited < 30*rate; waited += rate / 10 {
		l.send(make([]int16, rate/10))
	}
	return run{cond: "noise", events: l.events(), samples: len(x)}
}

// heardTurn is one turn the session detected.
type heardTurn struct {
	item           string
	from, to       int // the audio kept for it, in samples
	stoppedAt      int // when speech_stopped arrived
	transcript     string
	reply          *reply
	utterance      int  // which truth it belongs to, or -1 for noise
	stillListening bool // speech started and never stopped
}

// reply is an answer to a turn.
type reply struct {
	firstAudioAt int // when its first audio arrived, in samples streamed
	status       string
	sentences    []replySentence
	endToEndMs   float64
}

type replySentence struct {
	text  string
	audio []int16
}

// turnsOf reads the turns, their transcripts, and their answers from a
// run's events. pad is the prefix padding kept before speech.
func turnsOf(events []event, pad int) []*heardTurn {
	var turns []*heardTurn
	byItem := map[string]*heardTurn{}
	var answering *heardTurn
	responses := map[string]*reply{}
	var last *heardTurn // the last turn with a transcript, which an answer is to
	var sentence *replySentence
	for _, e := range events {
		f := e.fields
		switch e.Type {
		case "input_audio_buffer.speech_started":
			t := &heardTurn{item: str(f["item_id"]), from: max(0, ms2s(num(f["audio_start_ms"]))-pad), utterance: -1, stillListening: true}
			turns = append(turns, t)
			byItem[t.item] = t
		case "input_audio_buffer.speech_stopped":
			if t := byItem[str(f["item_id"])]; t != nil {
				t.to, t.stoppedAt, t.stillListening = ms2s(num(f["audio_end_ms"])), e.at, false
			}
		case "conversation.item.input_audio_transcription.completed":
			if t := byItem[str(f["item_id"])]; t != nil {
				t.transcript = str(f["transcript"])
				if strings.TrimSpace(t.transcript) != "" {
					last = t
				}
			}
		case "response.created":
			id := str(mapOf(f["response"])["id"])
			rp := &reply{firstAudioAt: -1}
			responses[id] = rp
			answering = last
			if answering != nil && answering.reply == nil {
				answering.reply = rp
			}
		case "response.audio_transcript.delta":
			if rp := responses[str(f["response_id"])]; rp != nil {
				rp.sentences = append(rp.sentences, replySentence{text: strings.TrimSpace(str(f["delta"]))})
				sentence = &rp.sentences[len(rp.sentences)-1]
			}
		case "response.audio.delta":
			if rp := responses[str(f["response_id"])]; rp != nil {
				if rp.firstAudioAt < 0 {
					rp.firstAudioAt = e.at
				}
				if len(rp.sentences) > 0 {
					sentence = &rp.sentences[len(rp.sentences)-1]
					sentence.audio = append(sentence.audio, decodeAudio(str(f["delta"]))...)
				}
			}
		case "response.done":
			resp := mapOf(f["response"])
			if rp := responses[str(resp["id"])]; rp != nil {
				rp.status = str(resp["status"])
				if lat := mapOf(mapOf(resp["metadata"])["toskar_latency"]); lat != nil {
					rp.endToEndMs = num(lat["end_to_end_ms"])
				}
			}
		}
	}
	return turns
}

// utteranceScore is how one utterance was heard.
type utteranceScore struct {
	ID      string `json:"id"`
	Outcome string `json:"outcome"` // whole, split, end_cut, start_cut, missed, never_ended
	Turns   int    `json:"turns"`
	// UnheardMs is speech left out of every turn: words cut off. Of it,
	// LostStartMs is before the first word heard, LostEndMs after the last.
	UnheardMs   int `json:"unheard_ms"`
	LostStartMs int `json:"lost_start_ms"`
	LostEndMs   int `json:"lost_end_ms"`
	// WaitMs is from the end of speech to Toskar deciding the turn ended.
	WaitMs int `json:"wait_ms"`
	// Live only.
	Transcript   string  `json:"transcript,omitempty"`
	WER          float64 `json:"wer,omitempty"`
	FirstAudioMs int     `json:"first_audio_ms,omitempty"`
	ReplyLang    string  `json:"reply_lang,omitempty"`
	VoiceChange  bool    `json:"voice_change,omitempty"`
	Reply        string  `json:"reply,omitempty"`
}

// score maps a run's turns to what was said.
func score(rn run, pad int, live bool) (us []utteranceScore, noise []*heardTurn) {
	turns := turnsOf(rn.events, pad)
	for _, t := range turns {
		if t.stillListening {
			t.to = rn.samples
		}
		best, bestOverlap := -1, 0
		for i, s := range rn.said {
			// The echo of the last word rings on for a moment.
			o := overlap(t.from, t.to, s.start, s.end+rate/2)
			if o > bestOverlap {
				best, bestOverlap = i, o
			}
		}
		t.utterance = best
		if best < 0 {
			noise = append(noise, t)
		}
	}
	for i, s := range rn.said {
		var mine []*heardTurn
		for _, t := range turns {
			if t.utterance == i {
				mine = append(mine, t)
			}
		}
		sc := utteranceScore{ID: s.u.ID, Turns: len(mine), WaitMs: -1}
		heard := make([]bool, len(s.active))
		first, last := -1, -1
		for f, on := range s.active {
			if !on {
				continue
			}
			a, b := s.start+f*frame, s.start+(f+1)*frame
			for _, t := range mine {
				if overlap(t.from, t.to, a, b) > frame/2 {
					heard[f] = true
					break
				}
			}
			if heard[f] {
				if first < 0 {
					first = f
				}
				last = f
			} else {
				sc.UnheardMs += 20
			}
		}
		for f, on := range s.active {
			if on && !heard[f] {
				if first < 0 || f < first {
					sc.LostStartMs += 20
				} else if f > last {
					sc.LostEndMs += 20
				}
			}
		}
		switch {
		case len(mine) == 0:
			sc.Outcome = "missed"
		case mine[len(mine)-1].stillListening:
			sc.Outcome = "never_ended"
		case len(mine) > 1:
			sc.Outcome = "split"
		case sc.LostEndMs >= cutMs:
			sc.Outcome = "end_cut"
		case sc.LostStartMs >= cutMs:
			sc.Outcome = "start_cut"
		default:
			sc.Outcome = "whole"
		}
		if n := len(mine); n > 0 && !mine[n-1].stillListening {
			sc.WaitMs = (mine[n-1].stoppedAt - s.end) * 1000 / rate
		}
		if live {
			var words []string
			var answered *heardTurn
			for _, t := range mine {
				if strings.TrimSpace(t.transcript) != "" {
					words = append(words, strings.TrimSpace(t.transcript))
				}
				if t.reply != nil {
					answered = t
				}
			}
			sc.Transcript = strings.Join(words, " ")
			sc.WER = wer(s.text, sc.Transcript)
			if answered != nil {
				rp := answered.reply
				if rp.firstAudioAt >= 0 {
					sc.FirstAudioMs = (rp.firstAudioAt - s.end) * 1000 / rate
				}
				var texts []string
				for _, st := range rp.sentences {
					texts = append(texts, st.text)
				}
				sc.Reply = strings.Join(texts, " ")
				sc.ReplyLang = replyLanguage(sc.Reply)
				sc.VoiceChange = voiceChanged(rp.sentences)
			}
		}
		us = append(us, sc)
	}
	return us, noise
}

// cutMs of speech left out of a turn is words lost: a syllable is about
// 150 ms.
const cutMs = 120

func overlap(a0, a1, b0, b1 int) int {
	return max(0, min(a1, b1)-max(a0, b0))
}

func ms2s(ms float64) int { return int(ms * rate / 1000) }

func median(xs []int) int {
	if len(xs) == 0 {
		return -1
	}
	s := append([]int(nil), xs...)
	sort.Ints(s)
	return s[len(s)/2]
}

// voiceChanged is whether one answer's sentences sound like different
// voices: the median pitch of one sentence 25% or more off the answer's
// first, as a switch between voices is.
func voiceChanged(sentences []replySentence) bool {
	first := 0.0
	for _, s := range sentences {
		p := medianPitch(s.audio)
		if p == 0 {
			continue
		}
		if first == 0 {
			first = p
			continue
		}
		if math.Abs(p-first)/first >= 0.25 {
			return true
		}
	}
	return false
}

// medianPitch is the median fundamental frequency of a recording's voiced
// 40 ms windows, by autocorrelation between 70 and 400 Hz; 0 for none.
func medianPitch(pcm []int16) float64 {
	x := toFloat(pcm)
	win := rate / 25
	var pitches []float64
	for at := 0; at+win+rate/70 < len(x); at += win / 2 {
		w := x[at : at+win]
		if rms(w) < dbAmp(-40) {
			continue
		}
		var e0 float64
		for _, v := range w {
			e0 += v * v
		}
		bestLag, best := 0, 0.0
		for lag := rate / 400; lag <= rate/70; lag++ {
			var c, e1 float64
			for i := 0; i < win; i++ {
				c += w[i] * x[at+i+lag]
				e1 += x[at+i+lag] * x[at+i+lag]
			}
			if n := c / math.Sqrt(e0*e1+1e-12); n > best {
				best, bestLag = n, lag
			}
		}
		if best > 0.6 {
			pitches = append(pitches, float64(rate)/float64(bestLag))
		}
	}
	if len(pitches) < 3 {
		return 0
	}
	sort.Float64s(pitches)
	return pitches[len(pitches)/2]
}
