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

// What a service can do with devices (#586, #667). A service offers any
// of them; the manager asks every connected one that does.
type (
	// DeviceLister lists a service's devices.
	DeviceLister interface {
		Devices(ctx context.Context, c *http.Client, cred Credential) ([]Device, error)
	}
	// CameraSource takes a camera's picture now: its bytes and type.
	CameraSource interface {
		CameraPicture(ctx context.Context, c *http.Client, cred Credential, device string) ([]byte, string, error)
	}
	// DeviceWatcher tells onChange of each state change of devices, as it
	// happens, until ctx ends or the connection drops.
	DeviceWatcher interface {
		WatchDevices(ctx context.Context, cred Credential, devices []string, onChange func(DeviceChange)) error
	}
)

// Devices are the connected services' devices, sorted by kind and name. A
// service that isn't connected has none; one that can't be read is
// skipped unless it's the only one.
func (m *Manager) Devices(ctx context.Context) ([]Device, error) {
	out := []Device{}
	var firstErr error
	for _, id := range m.serviceIDs() {
		s := m.services[id]
		lister, ok := s.(DeviceLister)
		if !ok {
			continue
		}
		cred, err := m.credential(id)
		if err != nil {
			continue
		}
		list, err := lister.Devices(ctx, m.client, cred)
		if err != nil {
			if firstErr == nil {
				firstErr = sanitizeErr(err, secretsOnly(s, cred))
			}
			continue
		}
		out = append(out, list...)
	}
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// Devices are Home Assistant's entities, by kind.
func (HomeAssistant) Devices(ctx context.Context, c *http.Client, cred Credential) ([]Device, error) {
	base, err := haBase(cred)
	if err != nil {
		return nil, err
	}
	var states []haState
	if err := call(ctx, c, http.MethodGet, base+"/api/states", haHeader(cred), nil, &states); err != nil {
		return nil, err
	}
	out := make([]Device, 0, len(states))
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
	return out, nil
}
