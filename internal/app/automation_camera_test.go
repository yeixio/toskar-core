package app

import (
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/automations"
)

// The run is told about its camera as a person would say it, and to answer
// from the picture, not the camera's status (#667).
func TestCamerasNote(t *testing.T) {
	note := camerasNote([]automations.Camera{{Connector: "homeassistant", Device: "camera.front_door_fluent"}})
	if strings.Contains(note, "camera.front_door") || !strings.Contains(note, "front door camera") || !strings.Contains(note, "status") {
		t.Fatalf("note %q", note)
	}
	two := camerasNote([]automations.Camera{{Device: "camera.front_door_clear"}, {Device: "camera.driveway"}})
	if !strings.Contains(two, "front door, driveway cameras") {
		t.Fatalf("two %q", two)
	}
	if camerasNote(nil) != "" {
		t.Fatal("a note with no cameras")
	}
}
