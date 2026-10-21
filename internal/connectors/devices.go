package connectors

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/net/websocket"
)

// DeviceChange is a connected service's device changing state (#586), such
// as a doorbell's visitor sensor going from off to on.
type DeviceChange struct {
	Device string
	From   string
	To     string
	At     time.Time
}

// WatchDevices tells onChange of each state change of devices, as it
// happens, until ctx ends or the connection drops; the caller connects
// again.
func (m *Manager) WatchDevices(ctx context.Context, service string, devices []string, onChange func(DeviceChange)) error {
	s, err := m.service(service)
	if err != nil {
		return err
	}
	watcher, ok := s.(DeviceWatcher)
	if !ok {
		return fmt.Errorf("%s has no devices to watch", s.Name())
	}
	cred, err := m.credential(service)
	if err != nil {
		return fmt.Errorf("%s is not connected. Connect it in Settings", s.Name())
	}
	if err := watcher.WatchDevices(ctx, cred, devices, onChange); err != nil && ctx.Err() == nil {
		return sanitizeErr(err, secretsOnly(s, cred))
	}
	return nil
}

// haPing is how often the watch asks Home Assistant whether it's there,
// and haQuiet how long it waits for any message before giving up.
var (
	haPing  = 30 * time.Second
	haQuiet = 90 * time.Second
)

// haMessage is the part of Home Assistant's WebSocket messages a watch reads.
type haMessage struct {
	ID      int    `json:"id"`
	Type    string `json:"type"`
	Success *bool  `json:"success"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error"`
	Event struct {
		Variables struct {
			Trigger struct {
				EntityID  string `json:"entity_id"`
				FromState *struct {
					State string `json:"state"`
				} `json:"from_state"`
				ToState *struct {
					State string `json:"state"`
				} `json:"to_state"`
			} `json:"trigger"`
		} `json:"variables"`
	} `json:"event"`
}

// haWatch subscribes to Home Assistant's state trigger for devices over its
// WebSocket API, so Home Assistant sends only their changes.
func haWatch(ctx context.Context, cred Credential, devices []string, onChange func(DeviceChange)) error {
	base, err := haBase(cred)
	if err != nil {
		return err
	}
	cfg, err := websocket.NewConfig("ws"+strings.TrimPrefix(base, "http")+"/api/websocket", base)
	if err != nil {
		return err
	}
	conn, err := cfg.DialContext(ctx)
	if err != nil {
		return fmt.Errorf("couldn't reach Home Assistant: %w", err)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	receive := func() (haMessage, error) {
		var msg haMessage
		_ = conn.SetReadDeadline(time.Now().Add(haQuiet))
		err := websocket.JSON.Receive(conn, &msg)
		return msg, err
	}
	if msg, err := receive(); err != nil || msg.Type != "auth_required" {
		return fmt.Errorf("no sign-in request from Home Assistant: %v", err)
	}
	if err := websocket.JSON.Send(conn, map[string]string{"type": "auth", "access_token": cred["token"]}); err != nil {
		return err
	}
	switch msg, err := receive(); {
	case err != nil:
		return err
	case msg.Type == "auth_invalid":
		return errors.New("the Home Assistant token was refused; connect it again in Settings")
	case msg.Type != "auth_ok":
		return fmt.Errorf("unexpected Home Assistant answer %q", msg.Type)
	}
	subscribe := map[string]any{"id": 1, "type": "subscribe_trigger",
		"trigger": map[string]any{"platform": "state", "entity_id": devices}}
	if err := websocket.JSON.Send(conn, subscribe); err != nil {
		return err
	}
	if msg, err := receive(); err != nil {
		return err
	} else if msg.Success == nil || !*msg.Success {
		why := "refused"
		if msg.Error != nil {
			why = msg.Error.Message
		}
		return fmt.Errorf("the devices weren't watched: Home Assistant said %s", why)
	}
	// Pings keep a quiet connection known to be alive.
	go func() {
		tick := time.NewTicker(haPing)
		defer tick.Stop()
		for id := 2; ; id++ {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if websocket.JSON.Send(conn, map[string]any{"id": id, "type": "ping"}) != nil {
					return
				}
			}
		}
	}()
	for {
		msg, err := receive()
		if err != nil {
			return err
		}
		if msg.Type != "event" || msg.ID != 1 {
			continue
		}
		t := msg.Event.Variables.Trigger
		change := DeviceChange{Device: t.EntityID, At: time.Now()}
		if t.FromState != nil {
			change.From = t.FromState.State
		}
		if t.ToState != nil {
			change.To = t.ToState.State
		}
		if change.Device != "" && change.From != change.To {
			onChange(change)
		}
	}
}
