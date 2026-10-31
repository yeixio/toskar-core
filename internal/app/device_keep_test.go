package app

import (
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/runlog"
)

// A model another device used stays loaded for an hour after (#498).
func TestDevicesKeepTheirModelLoadedForAnHour(t *testing.T) {
	now := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	a := &App{deviceClock: func() time.Time { return now }}
	if a.keptForDevices("llama") {
		t.Fatal("kept before any device used it")
	}
	a.noteDeviceUse("llama")
	now = now.Add(59 * time.Minute)
	if !a.keptForDevices("llama") || a.keptForDevices("other") {
		t.Fatal("within the hour")
	}
	now = now.Add(2 * time.Minute)
	if a.keptForDevices("llama") {
		t.Fatal("kept past the hour")
	}
}

func TestFromDevice(t *testing.T) {
	for _, c := range []struct {
		o    runlog.Origin
		want bool
	}{
		{runlog.Origin{Client: "watch"}, true},
		{runlog.Origin{Client: "car"}, true},
		{runlog.Origin{Client: "tv"}, true},
		{runlog.Origin{Client: "web"}, false},
		{runlog.Origin{Client: "desktop"}, false},
		{runlog.Origin{}, false},
		{runlog.Origin{Remote: true}, true},
	} {
		if got := fromDevice(c.o); got != c.want {
			t.Errorf("fromDevice(%+v) = %v", c.o, got)
		}
	}
}
