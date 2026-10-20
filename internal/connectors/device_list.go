package connectors

import (
	"context"
	"net/http"
	"sort"
	"strings"
)

// Device is one of a connected service's devices, by a kind that doesn't
// depend on the service (#586): what automations and, later, speakers and
// microphones pick from.
type Device struct {
	Connector string `json:"connector"`
	// Device is how the service names it, such as camera.driveway.
	Device string `json:"device"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	State  string `json:"state"`
}

// Device kinds.
const (
	KindCamera  = "camera"
	KindSensor  = "sensor"
	KindSpeaker = "speaker"
	KindLight   = "light"
	KindSwitch  = "switch"
	KindLock    = "lock"
	KindCover   = "cover"
	KindClimate = "climate"
	KindOther   = "other"
)

// haKinds are Home Assistant's domains by kind.
var haKinds = map[string]string{
	"camera": KindCamera, "binary_sensor": KindSensor, "sensor": KindSensor, "media_player": KindSpeaker,
	"light": KindLight, "switch": KindSwitch, "lock": KindLock, "cover": KindCover, "climate": KindClimate,
}

// Devices are the connected services' devices, sorted by kind and name.
// A service that isn't connected has none.
func (m *Manager) Devices(ctx context.Context) ([]Device, error) {
	out := []Device{}
	s, err := m.service("homeassistant")
	if err != nil {
		return out, nil
	}
	cred, err := m.credential("homeassistant")
	if err != nil {
		return out, nil
	}
	base, err := haBase(cred)
	if err != nil {
		return nil, err
	}
	var states []haState
	if err := call(ctx, m.client, http.MethodGet, base+"/api/states", haHeader(cred), nil, &states); err != nil {
		return nil, sanitizeErr(err, secretsOnly(s, cred))
	}
	for _, st := range states {
		domain, _, _ := strings.Cut(st.EntityID, ".")
		kind := haKinds[domain]
		if kind == "" {
			kind = KindOther
		}
		name, _ := st.Attributes["friendly_name"].(string)
		if name == "" {
			name = st.EntityID
		}
		out = append(out, Device{Connector: "homeassistant", Device: st.EntityID, Name: name, Kind: kind, State: st.State})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}
