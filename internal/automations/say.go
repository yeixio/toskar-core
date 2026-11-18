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
	if s.Device != "camera" {
		return fmt.Errorf("a Reolink camera's device is camera, not %q", s.Device)
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

// SayNote tells the run its answer is spoken at the door.
func SayNote(s *Say) string {
	if s == nil {
		return ""
	}
	return "Your answer is spoken aloud through the door's speaker to whoever is there, so write only the words to say: one or two short sentences, " +
		"with no formatting, lists, or links. If nobody is there to talk to, such as when the picture shows no one or only someone passing by, answer only " + SayNothing + "."
}
