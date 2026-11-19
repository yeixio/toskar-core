package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/connectors"
	"github.com/yeixio/toskar-core/internal/voice"
)

type fakeDoorVoice struct{ said []string }

func (v *fakeDoorVoice) Speak(_ context.Context, text, _, _ string) ([]int16, error) {
	v.said = append(v.said, text)
	return make([]int16, voice.SampleRate), nil // one second at 24 kHz
}

type fakeDoor struct {
	service string
	samples int
	rate    int
	err     error
}

func (d *fakeDoor) Talk(_ context.Context, service string, samples []int16, rate int) error {
	d.service, d.samples, d.rate = service, len(samples), rate
	return d.err
}

// An automation that speaks says its answer at the door, read by this
// computer's voice at the doorbell's rate; it says nothing when the run
// found nobody to talk to, or in quiet hours (#667).
func TestSayAnswerAtTheDoor(t *testing.T) {
	ctx := context.Background()
	auto := automations.Automation{
		Schedule: automations.Schedule{TimeZone: "UTC"},
		Say:      &automations.Say{Connector: "reolink", Device: "camera", QuietFrom: "22:00", QuietTo: "07:00"},
	}
	afternoon := time.Date(2026, 11, 18, 15, 0, 0, 0, time.UTC)
	sp, door := &fakeDoorVoice{}, &fakeDoor{}

	said, err := sayAnswer(ctx, sp, door, auto, "Hi there! **Mike** will be right down.", "en", afternoon)
	if err != nil || said != "Hi there! Mike will be right down." {
		t.Fatalf("said %q, %v", said, err)
	}
	if door.service != "reolink" || door.rate != connectors.ReolinkTalkRate || door.samples != connectors.ReolinkTalkRate {
		t.Fatalf("talked %+v", door)
	}

	*door = fakeDoor{}
	if said, err := sayAnswer(ctx, sp, door, auto, "NOTHING", "en", afternoon); err != nil || said != "" || door.service != "" {
		t.Fatalf("a run with nobody there said %q (%+v, %v)", said, door, err)
	}
	if said, err := sayAnswer(ctx, sp, door, auto, "Hello!", "en", time.Date(2026, 11, 18, 23, 30, 0, 0, time.UTC)); err != nil || said != "" || door.service != "" {
		t.Fatalf("quiet hours said %q (%+v, %v)", said, door, err)
	}
	if len(sp.said) != 1 {
		t.Fatalf("read aloud %v", sp.said)
	}

	door.err = errors.New("camera is offline")
	if _, err := sayAnswer(ctx, sp, door, auto, "Hello!", "en", afternoon); err == nil {
		t.Fatal("a doorbell that couldn't speak wasn't an error")
	}
}

// The run is told its answer is spoken, and how to say nothing (#667).
func TestSayNoteTellsTheRun(t *testing.T) {
	note := automations.SayNote(&automations.Say{Connector: "reolink", Device: "camera"})
	if note == "" || automations.SayNote(nil) != "" {
		t.Fatalf("note = %q", note)
	}
}

type voicedDoor struct{ voices []string }

func (v *voicedDoor) Speak(_ context.Context, _, voiceName, _ string) ([]int16, error) {
	v.voices = append(v.voices, voiceName)
	if voiceName == "kitten:en:gone" {
		return nil, errors.New("no such voice")
	}
	return make([]int16, voice.SampleRate), nil
}

// The door speaks in its own voice when one is chosen, and in the
// language's voice when that one can't read (#1304).
func TestSayAnswerInTheDoorsVoice(t *testing.T) {
	ctx := context.Background()
	afternoon := time.Date(2026, 11, 18, 15, 0, 0, 0, time.UTC)
	auto := automations.Automation{Say: &automations.Say{Connector: "reolink", Device: "camera", Voice: "kitten:en:eleanor"}}
	sp := &voicedDoor{}
	if _, err := sayAnswer(ctx, sp, &fakeDoor{}, auto, "Hello!", "en", afternoon); err != nil {
		t.Fatal(err)
	}
	auto.Say.Voice = "kitten:en:gone"
	if _, err := sayAnswer(ctx, sp, &fakeDoor{}, auto, "Hello!", "en", afternoon); err != nil {
		t.Fatal(err)
	}
	if strings.Join(sp.voices, ",") != "kitten:en:eleanor,kitten:en:gone," {
		t.Fatalf("voices %q", sp.voices)
	}
}
