package automations

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Say speaks each run's answer through a camera's speaker (#667), such as a
// Reolink doorbell's, so an automation that starts when someone is at the
// door can greet them. The run is told its answer is spoken, and answers
// SayNothing when there's nobody to talk to, such as a passer-by in the
// picture, and then nothing is said.
type Say struct {
	Connector string `json:"connector"`
	Device    string `json:"device"`
	// QuietFrom and QuietTo, such as "22:00" and "07:00", are the hours in
	// the automation's time zone when it says nothing: the run still
	// happens and notifies as it's set to. Both or neither.
	QuietFrom string `json:"quiet_from,omitempty"`
	QuietTo   string `json:"quiet_to,omitempty"`
	// Voice is the voice it speaks in, such as kitten:en:eleanor (#1304);
	// "" is the voice chosen for the answer's language in the Voice
	// settings. A voice that's no longer here leaves it to that one.
	Voice string `json:"voice,omitempty"`
	// Listen talks with the visitor after the first words (#667): it hears
	// them through the camera's microphone, answers, and sums the visit up
	// in the run's result, which is what it notifies with.
	Listen bool `json:"listen,omitempty"`
	// NoThinkingSound leaves out the soft chime that plays while an answer
	// to the visitor takes a while (#1334). It's on by default, and quiet
	// hours mute it too.
	NoThinkingSound bool `json:"no_thinking_sound,omitempty"`
}

// SayReolink is a Reolink camera's speaker, reached through reolink-cli.
const SayReolink = CameraReolink

// SayNothing is the answer that says nothing.
const SayNothing = "NOTHING"

// MaxSaid is the most characters said at once: a greeting, not a reading.
const MaxSaid = 400

// sayVoiceRe is a voice name: Kitten's, Kokoro's or Piper's.
var sayVoiceRe = regexp.MustCompile(`^[A-Za-z0-9_:-]{1,80}$`)

var clockRe = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// Clean trims the fields.
func (s *Say) Clean() {
	if s == nil {
		return
	}
	s.Connector = strings.TrimSpace(s.Connector)
	s.Device = strings.TrimSpace(s.Device)
	s.QuietFrom = strings.TrimSpace(s.QuietFrom)
	s.QuietTo = strings.TrimSpace(s.QuietTo)
	s.Voice = strings.TrimSpace(s.Voice)
}

// Validate checks the speaker can be spoken through.
func (s *Say) Validate() error {
	if s == nil {
		return nil
	}
	if s.Connector != SayReolink {
		return fmt.Errorf("unsupported speaker connector %q", s.Connector)
	}
	if !ReolinkDevice(s.Device, "camera") {
		return fmt.Errorf("a Reolink camera's device is camera, or camera@1 for a hub's channel 1, not %q", s.Device)
	}
	if s.Voice != "" && !sayVoiceRe.MatchString(s.Voice) {
		return fmt.Errorf("%q is not a voice name", s.Voice)
	}
	if (s.QuietFrom == "") != (s.QuietTo == "") {
		return fmt.Errorf("quiet hours need both a start and an end")
	}
	for _, v := range []string{s.QuietFrom, s.QuietTo} {
		if v != "" && !clockRe.MatchString(v) {
			return fmt.Errorf("quiet hours are times such as 22:00, not %q", v)
		}
	}
	if s.QuietFrom != "" && s.QuietFrom == s.QuietTo {
		return fmt.Errorf("quiet hours can't start and end at the same time")
	}
	return nil
}

// Quiet reports whether now falls in the quiet hours, in loc. Quiet hours
// that pass midnight, such as 22:00 to 07:00, run into the next morning.
func (s *Say) Quiet(now time.Time, loc *time.Location) bool {
	if s == nil || s.QuietFrom == "" {
		return false
	}
	if loc != nil {
		now = now.In(loc)
	}
	clock := now.Format("15:04")
	if s.QuietFrom < s.QuietTo {
		return clock >= s.QuietFrom && clock < s.QuietTo
	}
	return clock >= s.QuietFrom || clock < s.QuietTo
}

var (
	markRe  = regexp.MustCompile("[*_`#>~|]+")
	linkRe  = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	spaceRe = regexp.MustCompile(`\s+`)
)

// Spoken is an answer as it's said: without formatting, on one line, and
// cut at MaxSaid on a sentence's end when it can be. It's empty for
// SayNothing, or an answer with nothing to say.
func Spoken(answer string) string {
	text := linkRe.ReplaceAllString(answer, "$1")
	text = markRe.ReplaceAllString(text, "")
	text = strings.TrimSpace(spaceRe.ReplaceAllString(text, " "))
	bare := strings.Trim(strings.ToUpper(text), " .!\"'")
	if bare == "" || bare == SayNothing {
		return ""
	}
	if r := []rune(text); len(r) > MaxSaid {
		text = string(r[:MaxSaid])
		if i := strings.LastIndexAny(text, ".!?"); i > MaxSaid/2 {
			text = text[:i+1]
		}
	}
	return text
}

// Rang reports a trigger that is a doorbell's button: someone pressed it,
// so someone is there even when the picture shows no one (#1339), such as
// Reolink's doorbell or Home Assistant's binary_sensor.front_door_visitor.
func Rang(t *Trigger) bool {
	if t == nil || t.Kind != TriggerDevice {
		return false
	}
	d := strings.ToLower(t.Device)
	return d == "doorbell" || strings.Contains(d, "doorbell") || strings.Contains(d, "visitor")
}

// SayNote tells the run its answer is spoken at the door. rang is a run a
// doorbell's button started, where someone is there to greet even out of
// the picture, so the silent answer isn't offered (#1339).
func SayNote(s *Say, rang bool) string {
	if s == nil {
		return ""
	}
	note := "Your answer is spoken aloud through the door's speaker to whoever is there. First write only the words to say: one or two short sentences, " +
		"with no formatting, lists, or links. Then, on a new line starting with " + OwnerMark + ", write for the person this automation is for: " +
		"who is there and what they seem to want, from what you see. That line is never said at the door."
	if rang {
		note += " Someone pressed the doorbell, so someone is there even if the picture shows no one: greet them."
	} else {
		note += " If nobody is there to talk to, such as when the picture shows no one or only someone passing by, answer only " + SayNothing + "."
	}
	if s.Listen {
		note += " They can answer you, and you'll hear them: greet them and ask how you can help."
	}
	return note
}

// OwnerMark starts the part of a speaking run's answer that is for the
// automation's owner, not the visitor (#1339).
const OwnerMark = "FOR YOU:"

var ownerMarkRe = regexp.MustCompile(`(?im)^[\s*_#>-]*for you\s*[*_]*:[*_]*\s*`)

// SplitSay parts a speaking run's answer into the words for the door and
// the note for its owner (#1339). An answer without the mark is all for
// the door, with no note.
func SplitSay(answer string) (door, owner string) {
	loc := ownerMarkRe.FindStringIndex(answer)
	if loc == nil {
		return strings.TrimSpace(answer), ""
	}
	return strings.TrimSpace(answer[:loc[0]]), strings.TrimSpace(answer[loc[1]:])
}

// MaxVisitTurns is the most a visitor is answered in one visit.
const MaxVisitTurns = 6
