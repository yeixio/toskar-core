package automations

import (
	"strings"
	"testing"
	"time"
)

func TestSayValidate(t *testing.T) {
	ok := []Say{
		{Connector: "reolink", Device: "camera"},
		{Connector: "reolink", Device: "camera", QuietFrom: "22:00", QuietTo: "07:00"},
	}
	for _, s := range ok {
		if err := s.Validate(); err != nil {
			t.Errorf("%+v: %v", s, err)
		}
	}
	bad := map[string]Say{
		"other connector": {Connector: "homeassistant", Device: "camera"},
		"other device":    {Connector: "reolink", Device: "doorbell"},
		"half quiet":      {Connector: "reolink", Device: "camera", QuietFrom: "22:00"},
		"not a time":      {Connector: "reolink", Device: "camera", QuietFrom: "10pm", QuietTo: "07:00"},
		"same time":       {Connector: "reolink", Device: "camera", QuietFrom: "07:00", QuietTo: "07:00"},
	}
	for name, s := range bad {
		if err := s.Validate(); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	var none *Say
	if err := none.Validate(); err != nil {
		t.Errorf("nil: %v", err)
	}
}

func TestSayQuiet(t *testing.T) {
	night := &Say{Connector: "reolink", Device: "camera", QuietFrom: "22:00", QuietTo: "07:00"}
	day := &Say{Connector: "reolink", Device: "camera", QuietFrom: "12:00", QuietTo: "13:30"}
	at := func(h, m int) time.Time { return time.Date(2026, 11, 18, h, m, 0, 0, time.UTC) }
	cases := []struct {
		s     *Say
		when  time.Time
		quiet bool
	}{
		{night, at(21, 59), false},
		{night, at(22, 0), true},
		{night, at(3, 0), true},
		{night, at(6, 59), true},
		{night, at(7, 0), false},
		{day, at(12, 30), true},
		{day, at(13, 30), false},
		{&Say{Connector: "reolink", Device: "camera"}, at(3, 0), false},
	}
	for _, c := range cases {
		if got := c.s.Quiet(c.when, time.UTC); got != c.quiet {
			t.Errorf("%s–%s at %s: quiet = %v", c.s.QuietFrom, c.s.QuietTo, c.when.Format("15:04"), got)
		}
	}
	// The automation's zone decides: 23:00 in UTC is 15:00 in Los Angeles.
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Skip(err)
	}
	if night.Quiet(at(23, 0), la) {
		t.Error("23:00 UTC is the afternoon in Los Angeles")
	}
}

func TestSpoken(t *testing.T) {
	cases := map[string]string{
		"NOTHING":                           "",
		"  nothing. ":                       "",
		"**NOTHING**":                       "",
		"":                                  "",
		"Hi! **Mike** will be right there.": "Hi! Mike will be right there.",
		"See [our hours](https://x.y).":     "See our hours.",
		"Hello,\n\n- leave it by the door":  "Hello, - leave it by the door",
	}
	for in, want := range cases {
		if got := Spoken(in); got != want {
			t.Errorf("Spoken(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("This is a sentence. ", 40)
	got := Spoken(long)
	if len([]rune(got)) > MaxSaid || !strings.HasSuffix(got, ".") {
		t.Errorf("long answer cut to %d characters: %q", len(got), got[len(got)-20:])
	}
}

// A door that listens asks the visitor how it can help, so they answer
// (#667).
func TestSayNoteListens(t *testing.T) {
	if n := SayNote(&Say{Connector: SayReolink, Device: "camera"}, false); strings.Contains(n, "answer you") {
		t.Fatalf("a door that doesn't listen: %q", n)
	}
	if n := SayNote(&Say{Connector: SayReolink, Device: "camera", Listen: true}, false); !strings.Contains(n, "ask how you can help") {
		t.Fatalf("a door that listens: %q", n)
	}
}

// A doorbell's button means someone is there, even out of the picture:
// the run greets them and isn't offered the silent answer.
func TestSayNoteDoorbellRangGreets(t *testing.T) {
	// Regression: toskar-core#1339
	rang := Rang(&Trigger{Kind: TriggerDevice, Connector: "reolink", Device: "doorbell"})
	if !rang || !Rang(&Trigger{Kind: TriggerDevice, Connector: "homeassistant", Device: "binary_sensor.front_door_visitor"}) {
		t.Fatal("a doorbell's button isn't counted as a ring")
	}
	if Rang(&Trigger{Kind: TriggerDevice, Connector: "reolink", Device: "person"}) || Rang(nil) {
		t.Fatal("a person seen counts as a ring")
	}
	if n := SayNote(&Say{Connector: SayReolink, Device: "camera"}, true); strings.Contains(n, SayNothing) || !strings.Contains(n, "greet them") {
		t.Fatalf("a ring offers the silent answer: %q", n)
	}
	if n := SayNote(&Say{Connector: SayReolink, Device: "camera"}, false); !strings.Contains(n, SayNothing) {
		t.Fatalf("motion isn't offered the silent answer: %q", n)
	}
}
