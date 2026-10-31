package app

import (
	"time"

	"github.com/yeixio/toskar-core/internal/runlog"
)

// deviceKeepFor is how long after a phone, tablet, watch, car, or TV last used a
// model, or warmed it up, the idle sweeper leaves it loaded (#498). Its next
// question then starts at once instead of waiting for a load, which on the
// watch can use most of the time it has.
const deviceKeepFor = time.Hour

// fromDevice reports a chat asked from another device: the phone, tablet,
// watch, car (the phone app in CarPlay), or TV app, or anything from outside
// the home network.
func fromDevice(o runlog.Origin) bool {
	switch o.Client {
	case "phone", "tablet", "watch", "car", "tv":
		return true
	}
	return o.Remote
}

// noteDeviceUse records that another device used or warmed up modelID now.
func (a *App) noteDeviceUse(modelID string) {
	if modelID != "" {
		a.deviceUse.Store(modelID, a.deviceNow())
	}
}

// keptForDevices reports whether the idle sweeper should leave modelID
// loaded because another device used it within deviceKeepFor.
func (a *App) keptForDevices(modelID string) bool {
	v, ok := a.deviceUse.Load(modelID)
	if !ok {
		return false
	}
	if a.deviceNow().Sub(v.(time.Time)) > deviceKeepFor {
		a.deviceUse.Delete(modelID)
		return false
	}
	return true
}

func (a *App) deviceNow() time.Time {
	if a.deviceClock != nil {
		return a.deviceClock()
	}
	return time.Now()
}
