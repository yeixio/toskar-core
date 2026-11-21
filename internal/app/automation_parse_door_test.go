package app

import (
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/connectors"
)

var frontDoor = []connectors.Device{
	{Connector: "reolink", Device: "camera", Name: "Front Door", Kind: connectors.KindCamera},
	{Connector: "reolink", Device: "person", Name: "Front Door person", Kind: "sensor"},
	{Connector: "reolink", Device: "doorbell", Name: "Front Door doorbell button", Kind: "sensor"},
}

// A described doorbell automation starts from the doorbell's button,
// looks at its camera, and speaks at the door, instead of becoming an
// hourly schedule that looks at nothing (#1341).
func TestDoorRequestFromADescription(t *testing.T) {
	// Regression: toskar-core#1341
	schedule := eventSchedule("UTC")
	read, ok := doorRequest("When someone rings the doorbell, look at the camera, greet the visitor through the doorbell and tell me who is there.", frontDoor, schedule, "en")
	if !ok {
		t.Fatal("not read as a doorbell automation")
	}
	if read.Trigger == nil || read.Trigger.Kind != automations.TriggerDevice || read.Trigger.Device != "doorbell" || read.Trigger.Connector != "reolink" {
		t.Fatalf("trigger %+v", read.Trigger)
	}
	if len(read.Cameras) != 1 || read.Cameras[0].Device != "camera" {
		t.Fatalf("cameras %+v", read.Cameras)
	}
	if read.Say == nil || read.Say.Device != "camera" || !read.Say.Listen {
		t.Fatalf("say %+v", read.Say)
	}
	if read.Name != "Answer the door" || strings.Contains(read.Name, "_") {
		t.Fatalf("name %q", read.Name)
	}
	if read.Schedule.Validate() != nil {
		t.Fatalf("schedule %+v", read.Schedule)
	}
	if _, ok := doorRequest("When someone rings the doorbell, tell me.", nil, schedule, "en"); ok {
		t.Fatal("read as a doorbell automation with no doorbell connected")
	}
	if _, ok := doorRequest("Summarize the news.", frontDoor, schedule, "en"); ok {
		t.Fatal("a request with no doorbell in it")
	}
	quiet, _ := doorRequest("Wenn es an der Tür klingelt, schau auf die Kamera und sag mir Bescheid.", frontDoor, schedule, "de")
	if quiet.Trigger == nil || quiet.Say != nil {
		t.Fatalf("a request that doesn't greet: %+v %+v", quiet.Trigger, quiet.Say)
	}
}

// A model's reply names devices by id; only ones that exist are kept, and
// its snake_case name is written in words (#1341).
func TestModelRequestDevicesAndName(t *testing.T) {
	// Regression: toskar-core#1341
	read := withDevices(automations.ParsedRequest{}, map[string]any{
		"starts":            "reolink:doorbell",
		"cameras":           []any{"reolink:camera", "reolink:garage"},
		"speak_at_door":     true,
		"talk_with_visitor": false,
	}, frontDoor)
	if read.Trigger == nil || read.Trigger.Device != "doorbell" || len(read.Cameras) != 1 || read.Say == nil || read.Say.Listen {
		t.Fatalf("read %+v %+v %+v", read.Trigger, read.Cameras, read.Say)
	}
	if got := withDevices(automations.ParsedRequest{}, map[string]any{"starts": "reolink:camera"}, frontDoor); got.Trigger != nil {
		t.Fatal("a camera started it")
	}
	for in, want := range map[string]string{
		"doorbell_visitor_check": "Doorbell visitor check",
		"Morning news":           "Morning news",
		"price-watch":            "Price watch",
		"":                       "",
	} {
		if got := readableName(in); got != want {
			t.Errorf("readableName(%q) = %q", in, got)
		}
	}
	if n := devicesNote(frontDoor); !strings.Contains(n, "reolink:doorbell: Front Door doorbell button") {
		t.Fatalf("note %q", n)
	}
}
