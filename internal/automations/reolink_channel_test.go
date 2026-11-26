package automations

import "testing"

// A Reolink camera on a hub's other channel is named with @ and the
// channel, such as person@4, in triggers, cameras and speakers (#1333).
func TestReolinkDevicesNameTheirChannel(t *testing.T) {
	for _, device := range []string{"person", "person@4", "doorbell@0", "motion@23"} {
		tr := Trigger{Kind: TriggerDevice, Connector: "reolink", Device: device}
		if err := tr.Validate(); err != nil {
			t.Errorf("trigger %s: %v", device, err)
		}
	}
	for _, device := range []string{"camera@4", "person@", "person@x", "person@1234", "camera", "sofa@1"} {
		tr := Trigger{Kind: TriggerDevice, Connector: "reolink", Device: device}
		if tr.Validate() == nil {
			t.Errorf("trigger %s was taken", device)
		}
	}
	for _, device := range []string{"camera", "camera@4"} {
		if err := (Camera{Connector: CameraReolink, Device: device}).Validate(); err != nil {
			t.Errorf("camera %s: %v", device, err)
		}
		if err := (&Say{Connector: SayReolink, Device: device}).Validate(); err != nil {
			t.Errorf("speaker %s: %v", device, err)
		}
	}
	if (Camera{Connector: CameraReolink, Device: "person@4"}).Validate() == nil {
		t.Error("a detection was taken as a camera")
	}
	if !Rang(&Trigger{Kind: TriggerDevice, Connector: "reolink", Device: "doorbell@4"}) {
		t.Error("a hub doorbell's button isn't a ring")
	}
}
