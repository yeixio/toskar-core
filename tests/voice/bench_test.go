// Package voicebench measures live voice mode (#795) the way a person
// hears it: whether it lets you finish, keeps all your words, ignores the
// room's noise, and, against a running Toskar, how well it understood you,
// how soon it answered, and whether it answered in your language in one
// voice. README.md explains the scores.
package voicebench

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/voice"
)

var update = flag.Bool("update", false, "write this run's scores to baseline.json")

// seed makes every run's noise the same, so a change in score is a change
// in Toskar.
const seed = 795

// conditionScore is one condition's result.
type conditionScore struct {
	Condition  string `json:"condition"`
	Utterances int    `json:"utterances"`
	// Whole is turns heard as one turn with every word: the score.
	Whole int `json:"whole"`
	// Split is turns Toskar ended at a mid-sentence pause and answered
	// too early; EndCut lost their last words, StartCut their first;
	// Missed weren't heard; NeverEnded didn't end while the noise went on.
	Split      int `json:"split"`
	EndCut     int `json:"end_cut"`
	StartCut   int `json:"start_cut"`
	Missed     int `json:"missed"`
	NeverEnded int `json:"never_ended"`
	// NoiseTurns started without anyone talking.
	NoiseTurns int `json:"noise_turns"`
	// UnheardMs is speech left out of turns, summed.
	UnheardMs int `json:"unheard_ms"`
	// WaitMs is the median time from the end of speech to the turn ending.
	WaitMs int `json:"wait_ms"`

	// Live only: word error rate in percent, words transcribed from noise,
	// the median time from the end of speech to the first audio of the
	// answer, answers in another language, and answers that changed voice.
	WERPct       float64 `json:"wer_pct,omitempty"`
	NoiseWords   int     `json:"noise_words,omitempty"`
	FirstAudioMs int     `json:"first_audio_ms,omitempty"`
	WrongLang    int     `json:"wrong_language,omitempty"`
	VoiceChanges int     `json:"voice_changes,omitempty"`

	Details []utteranceScore `json:"details,omitempty"`
	Noise   []string         `json:"noise,omitempty"`
}

// localLink is a session in this process, with Toskar's own turn
// detection, and speech to text that writes nothing: turn-taking only.
type localLink struct {
	s    *voice.Session
	mu   sync.Mutex
	pos  int
	evs  []event
	open int
}

type silentEars struct{}

func (silentEars) Transcribe(context.Context, []byte, string) (string, error) { return "", nil }

func newLocalLink() *localLink {
	l := &localLink{}
	l.s = voice.NewSession(voice.Parts{Transcriber: silentEars{}}, func(e map[string]any) error {
		l.mu.Lock()
		defer l.mu.Unlock()
		kind := str(e["type"])
		switch kind {
		case "input_audio_buffer.speech_started":
			l.open++
		case "input_audio_buffer.speech_stopped":
			l.open--
		}
		l.evs = append(l.evs, event{Type: kind, at: l.pos, fields: e})
		return nil
	})
	return l
}

func (l *localLink) send(pcm []int16) {
	b, _ := json.Marshal(map[string]any{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(voice.EncodePCM16(pcm))})
	l.mu.Lock()
	l.pos += len(pcm)
	l.mu.Unlock()
	l.s.Handle(b)
}

func (l *localLink) events() []event {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]event(nil), l.evs...)
}

func (l *localLink) busy() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.open > 0
}

func (l *localLink) close() { l.s.Close() }

// pad is the session's default prefix padding, 300 ms.
const pad = 300 * rate / 1000

// bench runs every condition and the noise scene over links from dial.
func bench(t *testing.T, dial func() link, live bool) []conditionScore {
	clips, err := loadClips("clips")
	if err != nil {
		t.Fatal(err)
	}
	var out []conditionScore
	for i, c := range conditions {
		l := dial()
		rn := converse(l, c, clips, seed+uint64(i), 2*time.Second)
		l.close()
		us, noise := score(rn, pad, live)
		cs := summarize(c.Name, us, noise, live)
		out = append(out, cs)
	}
	l := dial()
	rn := listenNoise(l, clips, seed)
	l.close()
	_, noise := score(rn, pad, live)
	out = append(out, summarize("noise", nil, noise, live))
	return out
}

func summarize(name string, us []utteranceScore, noise []*heardTurn, live bool) conditionScore {
	cs := conditionScore{Condition: name, Utterances: len(us), NoiseTurns: len(noise), Details: us}
	var waits, firsts []int
	var errs, refs float64
	for _, u := range us {
		switch u.Outcome {
		case "whole":
			cs.Whole++
		case "split":
			cs.Split++
		case "end_cut":
			cs.EndCut++
		case "start_cut":
			cs.StartCut++
		case "missed":
			cs.Missed++
		case "never_ended":
			cs.NeverEnded++
		}
		cs.UnheardMs += u.UnheardMs
		if u.WaitMs >= 0 {
			waits = append(waits, u.WaitMs)
		}
		if live {
			if u.FirstAudioMs > 0 {
				firsts = append(firsts, u.FirstAudioMs)
			}
			if u.VoiceChange {
				cs.VoiceChanges++
			}
		}
	}
	cs.WaitMs = median(waits)
	for _, n := range noise {
		desc := fmt.Sprintf("%.1f–%.1f s", float64(n.from)/rate, float64(n.to)/rate)
		if live {
			cs.NoiseWords += len(words(n.transcript))
			desc += fmt.Sprintf(" %q", n.transcript)
		}
		cs.Noise = append(cs.Noise, desc)
	}
	if live {
		cs.FirstAudioMs = median(firsts)
		for _, u := range us {
			ref := float64(len(words(utteranceText(u.ID))))
			errs += u.WER * ref
			refs += ref
			if u.ReplyLang != "" && u.ReplyLang != utteranceLang(u.ID) {
				cs.WrongLang++
			}
		}
		if refs > 0 {
			cs.WERPct = round1(100 * errs / refs)
		}
	}
	return cs
}

var clipTexts map[string]*clip

func utteranceText(id string) string {
	for _, u := range utterances {
		if u.ID == id {
			return u.text(clipTexts)
		}
	}
	return ""
}

func utteranceLang(id string) string {
	for _, u := range utterances {
		if u.ID == id {
			return u.Lang
		}
	}
	return ""
}

func round1(x float64) float64 { return float64(int(x*10+0.5)) / 10 }

// TestTurnTaking runs the conversations through Toskar's own turn
// detection, in this process, and fails when a condition gets worse than
// baseline.json: fewer turns heard whole, or more turns from noise. A
// change that does better should write the new scores with -update, so
// they're kept.
func TestTurnTaking(t *testing.T) {
	var err error
	clipTexts, err = loadClips("clips")
	if err != nil {
		t.Fatal(err)
	}
	got := bench(t, func() link { return newLocalLink() }, false)
	report := table(got, false)
	t.Log("\n" + report)
	if p := os.Getenv("TOSKAR_VOICE_REPORT"); p != "" {
		_ = os.WriteFile(p, []byte(report), 0o644)
	}
	if *update {
		writeBaseline(t, "baseline.json", got)
		return
	}
	base := readBaseline(t, "baseline.json")
	for _, g := range got {
		b, ok := base[g.Condition]
		if !ok {
			t.Errorf("%s isn't in baseline.json; run with -update", g.Condition)
			continue
		}
		if g.Whole < b.Whole {
			t.Errorf("%s: %d of %d turns heard whole, down from %d: %s", g.Condition, g.Whole, g.Utterances, b.Whole, worse(g, b))
		}
		if g.NoiseTurns > b.NoiseTurns {
			t.Errorf("%s: %d turns started by noise, up from %d: %v", g.Condition, g.NoiseTurns, b.NoiseTurns, g.Noise)
		}
		if g.Whole > b.Whole || g.NoiseTurns < b.NoiseTurns {
			t.Logf("%s is better than baseline.json; keep it with go test ./tests/voice -run TestTurnTaking -update", g.Condition)
		}
	}
}

// worse names the utterances not heard whole.
func worse(g, b conditionScore) string {
	was := map[string]string{}
	for _, d := range b.Details {
		was[d.ID] = d.Outcome
	}
	var out []string
	for _, d := range g.Details {
		if d.Outcome != "whole" && was[d.ID] == "whole" {
			out = append(out, fmt.Sprintf("%s now %s (%d ms unheard, %d turns)", d.ID, d.Outcome, d.UnheardMs, d.Turns))
		}
	}
	return strings.Join(out, "; ")
}

func readBaseline(t *testing.T, path string) map[string]conditionScore {
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var list struct {
		Conditions []conditionScore `json:"conditions"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	out := map[string]conditionScore{}
	for _, c := range list.Conditions {
		out[c.Condition] = c
	}
	return out
}

func writeBaseline(t *testing.T, path string, got []conditionScore) {
	b, err := json.MarshalIndent(map[string]any{
		"about":      "Live voice turn-taking scores (tests/voice/README.md). TestTurnTaking fails when a condition gets worse; write better scores with go test ./tests/voice -run TestTurnTaking -update.",
		"conditions": got,
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Clean(path), append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// table is the scores as Markdown.
func table(cs []conditionScore, live bool) string {
	var b strings.Builder
	head := "| Condition | Heard whole | Answered mid-sentence | Last words cut off | First words cut off | Missed | Never ended | Turns from noise | Unheard speech | Wait after speech |"
	sep := "| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |"
	if live {
		head += " Word errors | Words from noise | First audio | Wrong language | Voice changed |"
		sep += " --- | --- | --- | --- | --- |"
	}
	b.WriteString(head + "\n" + sep + "\n")
	for _, c := range cs {
		whole := fmt.Sprintf("%d of %d", c.Whole, c.Utterances)
		if c.Utterances == 0 {
			whole = "–"
		}
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %d | %d | %d | %d | %d ms | %s |", c.Condition, whole, c.Split, c.EndCut, c.StartCut, c.Missed, c.NeverEnded, c.NoiseTurns, c.UnheardMs, msOrDash(c.WaitMs))
		if live {
			fmt.Fprintf(&b, " %.1f%% | %d | %s | %d | %d |", c.WERPct, c.NoiseWords, msOrDash(c.FirstAudioMs), c.WrongLang, c.VoiceChanges)
		}
		b.WriteString("\n")
	}
	b.WriteString("\nNot heard whole:\n\n")
	listed := false
	for _, c := range cs {
		for _, d := range c.Details {
			if d.Outcome == "whole" {
				continue
			}
			listed = true
			fmt.Fprintf(&b, "- %s, %s: %s, %d turns, %d ms unheard (%d ms of the start, %d ms of the end)", c.Condition, d.ID, outcomes[d.Outcome], d.Turns, d.UnheardMs, d.LostStartMs, d.LostEndMs)
			if live && d.Transcript != "" {
				fmt.Fprintf(&b, ", heard %q", d.Transcript)
			}
			b.WriteString("\n")
		}
		for _, n := range c.Noise {
			listed = true
			fmt.Fprintf(&b, "- %s: a turn from noise at %s\n", c.Condition, n)
		}
	}
	if !listed {
		b.WriteString("- none\n")
	}
	return b.String()
}

var outcomes = map[string]string{
	"whole": "heard whole", "split": "answered mid-sentence", "end_cut": "last words cut off",
	"start_cut": "first words cut off", "missed": "not heard", "never_ended": "turn never ended",
}

func msOrDash(ms int) string {
	if ms < 0 {
		return "–"
	}
	return fmt.Sprintf("%d ms", ms)
}
