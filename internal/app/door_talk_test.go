package app

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/connectors"
	"github.com/yeixio/toskar-core/internal/voice"
)

// visitDoor is a doorbell with someone at it: each time the door has
// spoken, the visitor says the next thing, as audio through its microphone.
type visitDoor struct {
	audio   chan []int16
	mu      sync.Mutex
	said    []string
	spoke   chan struct{}
	visitor int // how many things the visitor says
}

func (d *visitDoor) Listen(context.Context, string) (*connectors.Hearing, error) {
	go func() {
		for i := 0; i < d.visitor; i++ {
			if i > 0 {
				<-d.spoke
			}
			time.Sleep(60 * time.Millisecond) // after the door's echo
			d.send(0, 10)                     // quiet
			d.send(6000, 25)                  // half a second of talking
			d.send(0, 60)                     // and quiet again
		}
	}()
	return &connectors.Hearing{Audio: d.audio}, nil
}

// send is n 20 ms chunks of a tone at level.
func (d *visitDoor) send(level int16, n int) {
	for range n {
		chunk := make([]int16, connectors.ReolinkListenRate/50)
		for i := range chunk {
			if i%2 == 0 {
				chunk[i] = level
			} else {
				chunk[i] = -level
			}
		}
		d.audio <- chunk
	}
}

func (d *visitDoor) Talk(_ context.Context, _ string, samples []int16, _ int) error {
	d.spoke <- struct{}{}
	return nil
}

type scriptedEars struct {
	heard []string
	n     int
}

func (s *scriptedEars) Transcribe(_ context.Context, wav []byte, _ string) (string, error) {
	samples, _, err := voice.ParseWAV(wav)
	if err != nil || len(samples) < connectors.ReolinkListenRate/4 {
		return "", err
	}
	s.n++
	return s.heard[s.n-1], nil
}

// The door talks with a visitor: it hears each thing they say once they
// stop, answers it through the speaker without the mark that ends the
// visit, and ends when its answer has the mark; with nobody answering, the
// visit is only the greeting (#667).
func TestDoorVisit(t *testing.T) {
	ctx := context.Background()
	auto := automations.Automation{Schedule: automations.Schedule{TimeZone: "UTC"},
		Say: &automations.Say{Connector: automations.SayReolink, Device: "camera", Listen: true}}
	door := &visitDoor{audio: make(chan []int16, 1000), spoke: make(chan struct{}, 10), visitor: 2}
	speaker := &fakeDoorVoice{}
	ears := &scriptedEars{heard: []string{"Hi, I'm Sam from the post office.", "I have a parcel that needs a signature."}}
	var asked [][]doorLine
	answers := []string{"Hello Sam, what can I help with?", "Thanks, I'll let them know. Please leave a card. [done]"}
	v := &doorVisit{ear: door, talk: door, parts: voice.Parts{Transcriber: ears, Speaker: speaker}, automation: auto, lang: "en",
		reply: func(_ context.Context, lines []doorLine) (string, error) {
			asked = append(asked, append([]doorLine(nil), lines...))
			return answers[len(asked)-1], nil
		},
		wait: 2 * time.Second, longest: 5 * time.Second, echo: 30 * time.Millisecond}
	lines, err := v.run(ctx, "Hello there, can I help you?")
	if err != nil {
		t.Fatal(err)
	}
	want := "Toskar: Hello there, can I help you?\nVisitor: Hi, I'm Sam from the post office.\nToskar: Hello Sam, what can I help with?\n" +
		"Visitor: I have a parcel that needs a signature.\nToskar: Thanks, I'll let them know. Please leave a card."
	if got := transcript(lines); got != want {
		t.Fatalf("the visit:\n%s", got)
	}
	if len(asked) != 2 || len(asked[1]) != 4 || !asked[1][3].Visitor {
		t.Fatalf("the model was asked %+v", asked)
	}
	if len(speaker.said) != 2 || strings.Contains(speaker.said[1], "done") {
		t.Fatalf("said %q", speaker.said)
	}

	// Nobody answers the greeting.
	quiet := &visitDoor{audio: make(chan []int16, 1000), spoke: make(chan struct{}, 10)}
	v = &doorVisit{ear: quiet, talk: quiet, parts: voice.Parts{Transcriber: &scriptedEars{}, Speaker: speaker}, automation: auto, lang: "en",
		reply: func(context.Context, []doorLine) (string, error) { t.Fatal("answered nobody"); return "", nil },
		wait:  100 * time.Millisecond, longest: time.Second, echo: 10 * time.Millisecond}
	lines, err = v.run(ctx, "Hello?")
	if err != nil || len(lines) != 1 {
		t.Fatalf("nobody there: %+v, %v", lines, err)
	}
}

// The visitor is answered by the run's model, with the visit so far as the
// chat and how to talk at the door in its instructions; the visit is summed
// up for the people who live there from what was said (#667).
func TestDoorAnswer(t *testing.T) {
	a, used, sent := pictureApp(t, true)
	ctx := context.Background()
	auto := automations.Automation{ProfileID: "general-assistant"}
	lines := []doorLine{{Text: "Hello, can I help?"}, {Visitor: true, Text: "I'm Sam, with a parcel."}}
	if _, err := a.doorAnswer(ctx, auto, "text-model", doorTalkStyle, lines); err != nil {
		t.Fatal(err)
	}
	if len(*used) != 1 || (*used)[0] != "text-model" {
		t.Fatalf("answered with %v", *used)
	}
	msgs := (*sent)[0]
	var system, convo []string
	for _, m := range msgs {
		if m.Role == "system" {
			system = append(system, m.Content)
		} else {
			convo = append(convo, m.Role+": "+m.Content)
		}
	}
	if !strings.Contains(strings.Join(system, "\n"), "Never say whether anyone is home") {
		t.Fatalf("instructions: %q", system)
	}
	if strings.Join(convo, "\n") != "assistant: Hello, can I help?\nuser: I'm Sam, with a parcel." {
		t.Fatalf("the visit as the chat: %q", convo)
	}

	if _, err := a.doorAnswer(ctx, auto, "text-model", doorSummaryStyle, nil, transcript(lines)); err != nil {
		t.Fatal(err)
	}
	last := (*sent)[1]
	if u := last[len(last)-1]; u.Role != "user" || !strings.Contains(u.Content, "Visitor: I'm Sam, with a parcel.") {
		t.Fatalf("summed up from %+v", u)
	}
}
