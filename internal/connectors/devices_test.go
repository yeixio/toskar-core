package connectors

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

// fakeHAWebSocket is Home Assistant's WebSocket API, enough for a watch:
// sign-in with token, a state-trigger subscription, then the changes.
func fakeHAWebSocket(t *testing.T, token string, changes [][3]string, gotEntities *[]string) *httptest.Server {
	t.Helper()
	ws := websocket.Server{Handler: func(conn *websocket.Conn) {
		_ = websocket.JSON.Send(conn, map[string]string{"type": "auth_required"})
		var auth map[string]string
		if websocket.JSON.Receive(conn, &auth) != nil {
			return
		}
		if auth["access_token"] != token {
			_ = websocket.JSON.Send(conn, map[string]string{"type": "auth_invalid"})
			return
		}
		_ = websocket.JSON.Send(conn, map[string]string{"type": "auth_ok"})
		var sub struct {
			Type    string `json:"type"`
			Trigger struct {
				EntityID []string `json:"entity_id"`
			} `json:"trigger"`
		}
		if websocket.JSON.Receive(conn, &sub) != nil || sub.Type != "subscribe_trigger" {
			return
		}
		*gotEntities = sub.Trigger.EntityID
		_ = websocket.JSON.Send(conn, map[string]any{"id": 1, "type": "result", "success": true})
		for _, c := range changes {
			_ = websocket.JSON.Send(conn, map[string]any{"id": 1, "type": "event", "event": map[string]any{"variables": map[string]any{"trigger": map[string]any{
				"entity_id": c[0], "from_state": map[string]string{"state": c[1]}, "to_state": map[string]string{"state": c[2]}}}}})
		}
		// Stay open until the watch goes.
		var x any
		_ = websocket.JSON.Receive(conn, &x)
	}}
	mux := http.NewServeMux()
	mux.Handle("/api/websocket", ws)
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"location_name":"Home"}`))
	})
	mux.HandleFunc("/api/states", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`[]`)) })
	return httptest.NewServer(mux)
}

// A watch signs in, has Home Assistant watch only the devices asked for,
// and reports each real change as it comes (#586).
func TestWatchHomeAssistantDevices(t *testing.T) {
	const token = "ha-long-lived-token-xyz"
	var entities []string
	srv := fakeHAWebSocket(t, token, [][3]string{
		{"binary_sensor.front_door_visitor", "off", "on"},
		{"binary_sensor.front_door_visitor", "on", "on"}, // an attribute changed, not the state
		{"binary_sensor.front_door_visitor", "on", "off"},
	}, &entities)
	defer srv.Close()
	m, _, _ := newManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.WatchDevices(ctx, "homeassistant", []string{"binary_sensor.front_door_visitor"}, func(DeviceChange) {}); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("before connecting: %v", err)
	}
	if _, err := m.Connect(ctx, "homeassistant", map[string]string{"url": srv.URL, "token": token}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var got []DeviceChange
	done := make(chan error, 1)
	go func() {
		done <- m.WatchDevices(ctx, "homeassistant", []string{"binary_sensor.front_door_visitor"}, func(c DeviceChange) {
			mu.Lock()
			got = append(got, c)
			n := len(got)
			mu.Unlock()
			if n == 2 {
				cancel()
			}
		})
	}()
	if err := <-done; err != nil {
		t.Fatalf("watch: %v", err)
	}
	if len(entities) != 1 || entities[0] != "binary_sensor.front_door_visitor" {
		t.Fatalf("subscribed to %v", entities)
	}
	if len(got) != 2 || got[0].From != "off" || got[0].To != "on" || got[1].To != "off" {
		t.Fatalf("changes %+v", got)
	}
}
