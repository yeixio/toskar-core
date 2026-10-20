package app

import (
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/connectors"
)

// A device trigger runs on its device changing to the state it waits for,
// or any change without one, and once per cooldown (#586).
func TestDeviceTriggerMatchesAndCools(t *testing.T) {
	visitor := automations.Trigger{Kind: automations.TriggerDevice, Connector: "homeassistant", Device: "binary_sensor.front_door_visitor", To: "on"}
	press := connectors.DeviceChange{Device: "binary_sensor.front_door_visitor", From: "off", To: "on"}
	if !deviceMatches(visitor, "homeassistant", press) {
		t.Fatal("a press didn't match")
	}
	for name, c := range map[string]connectors.DeviceChange{
		"going off":      {Device: press.Device, From: "on", To: "off"},
		"another device": {Device: "binary_sensor.driveway_motion", From: "off", To: "on"},
	} {
		if deviceMatches(visitor, "homeassistant", c) {
			t.Fatalf("%s matched", name)
		}
	}
	any := visitor
	any.To = ""
	if !deviceMatches(any, "homeassistant", connectors.DeviceChange{Device: press.Device, From: "on", To: "off"}) {
		t.Fatal("a trigger with no state didn't match a change")
	}
	if deviceMatches(visitor, "github", press) {
		t.Fatal("matched another service's device")
	}

	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	d := &deviceTriggers{last: map[string]time.Time{}, now: func() time.Time { return now }}
	if !d.due("a", time.Minute) || d.due("a", time.Minute) {
		t.Fatal("a second press within the cooldown ran it again")
	}
	if !d.due("b", time.Minute) {
		t.Fatal("one automation's cooldown held another")
	}
	now = now.Add(61 * time.Second)
	if !d.due("a", time.Minute) {
		t.Fatal("didn't run after the cooldown")
	}
	if !sameDevices(map[string][]string{"homeassistant": {"a", "b"}}, map[string][]string{"homeassistant": {"a", "b"}}) ||
		sameDevices(map[string][]string{"homeassistant": {"a"}}, nil) {
		t.Fatal("sameDevices")
	}
}
