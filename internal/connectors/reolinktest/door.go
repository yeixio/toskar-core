// Package reolinktest is a simulated Reolink doorbell (#667): the parts of
// a camera's HTTP API Toskar uses, so the front door can be tested without
// a camera. A test makes someone walk up or ring, and reads what Toskar
// asked of the camera.
package reolinktest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// Picture is the JPEG the door's camera shows.
var Picture = []byte{0xff, 0xd8, 0xff, 0xe0, 'd', 'o', 'o', 'r'}

// Door is a simulated Reolink doorbell.
type Door struct {
	*httptest.Server
	User, Password string
	// Name and Model are what GetDevInfo answers.
	Name, Model string
	// Old answers only GetAiState and GetMdState, as cameras without
	// GetEvents do.
	Old bool

	mu                                sync.Mutex
	person, motion, doorbell, vehicle bool
	tokens                            map[string]bool
	logins                            int
	calls                             []string
}

// New starts a door with user admin and password door-pass. TLS makes it
// an HTTPS camera with its own certificate, as a Reolink camera has.
func New(tls bool) *Door {
	d := &Door{User: "admin", Password: "door-pass", Name: "Front Door", Model: "Reolink Video Doorbell PoE", tokens: map[string]bool{}}
	if tls {
		d.Server = httptest.NewTLSServer(http.HandlerFunc(d.serve))
	} else {
		d.Server = httptest.NewServer(http.HandlerFunc(d.serve))
	}
	return d
}

// Visit has a person walk up (true) or leave (false), with motion.
func (d *Door) Visit(here bool) {
	d.mu.Lock()
	d.person, d.motion = here, here
	d.mu.Unlock()
}

// Ring presses the doorbell button (true) or lets it go (false).
func (d *Door) Ring(pressed bool) {
	d.mu.Lock()
	d.doorbell = pressed
	d.mu.Unlock()
}

// Forget drops every sign-in, as a camera that restarted does.
func (d *Door) Forget() {
	d.mu.Lock()
	d.tokens = map[string]bool{}
	d.mu.Unlock()
}

// Logins is how many times Toskar signed in.
func (d *Door) Logins() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.logins
}

// Calls are the commands Toskar sent, in order.
func (d *Door) Calls() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.calls...)
}

func b(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (d *Door) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/cgi-bin/api.cgi" {
		http.NotFound(w, r)
		return
	}
	cmd := r.URL.Query().Get("cmd")
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, cmd)
	reply := func(value any) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{"cmd": cmd, "code": 0, "value": value}})
	}
	fail := func(code int, detail string) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{"cmd": cmd, "code": 1, "error": map[string]any{"rspCode": code, "detail": detail}}})
	}
	if cmd == "Login" {
		raw, _ := io.ReadAll(r.Body)
		var body []struct {
			Param struct {
				User struct {
					UserName string `json:"userName"`
					Password string `json:"password"`
				} `json:"User"`
			} `json:"param"`
		}
		if json.Unmarshal(raw, &body) != nil || len(body) == 0 || body[0].Param.User.UserName != d.User || body[0].Param.User.Password != d.Password {
			fail(-7, "login failed")
			return
		}
		d.logins++
		token := fmt.Sprintf("tok%d", d.logins)
		d.tokens[token] = true
		reply(map[string]any{"Token": map[string]any{"name": token, "leaseTime": 3600}})
		return
	}
	if !d.tokens[r.URL.Query().Get("token")] {
		fail(-6, "please login first")
		return
	}
	switch cmd {
	case "GetDevInfo":
		reply(map[string]any{"DevInfo": map[string]any{"name": d.Name, "model": d.Model, "type": "BELL"}})
	case "GetEvents":
		if d.Old {
			fail(-9, "not support")
			return
		}
		reply(map[string]any{
			"ai": map[string]any{
				"people":  map[string]any{"alarm_state": b(d.person), "support": 1},
				"vehicle": map[string]any{"alarm_state": b(d.vehicle), "support": 1},
				"dog_cat": map[string]any{"alarm_state": 0, "support": 0},
			},
			"md":      map[string]any{"alarm_state": b(d.motion), "support": 1},
			"visitor": map[string]any{"alarm_state": b(d.doorbell), "support": 1},
		})
	case "GetAiState":
		reply(map[string]any{"channel": 0, "people": map[string]any{"alarm_state": b(d.person), "support": 1},
			"vehicle": map[string]any{"alarm_state": b(d.vehicle), "support": 1}})
	case "GetMdState":
		reply(map[string]any{"state": b(d.motion)})
	case "Snap":
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(Picture)
	default:
		fail(-9, "not support "+strings.ToLower(cmd))
	}
}
