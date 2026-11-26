// Package reolinktest is a simulated Reolink doorbell (#667), alone or on a
// Home Hub with more cameras (#1333): the parts of
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
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Picture is the JPEG the door's camera shows.
var Picture = []byte{0xff, 0xd8, 0xff, 0xe0, 'd', 'o', 'o', 'r'}

// PictureOf is the JPEG the camera on a hub's channel shows: Picture for
// the door's own, channel 0.
func PictureOf(channel int) []byte {
	if channel == 0 {
		return Picture
	}
	return append(append([]byte(nil), Picture...), byte('0'+channel))
}

// Cam is another camera paired to the door's hub, such as a tracking
// camera on channel 4 (#1333).
type Cam struct {
	Channel int
	Name    string
	Online  bool
}

// state is what one camera sees.
type state struct{ person, motion, doorbell, vehicle bool }

// Door is a simulated Reolink doorbell.
type Door struct {
	*httptest.Server
	User, Password string
	// Name and Model are what GetDevInfo answers.
	Name, Model string
	// Old answers only GetAiState and GetMdState, as cameras without
	// GetEvents do.
	Old bool
	// Cams makes the door a Home Hub's channel 0, with these cameras paired
	// beside it; none is a doorbell on its own, with no channel status.
	Cams []Cam

	mu     sync.Mutex
	seen   map[int]*state
	tokens map[string]bool
	logins int
	calls  []string
}

// at is what the camera on channel sees, held with mu.
func (d *Door) at(channel int) *state {
	if d.seen == nil {
		d.seen = map[int]*state{}
	}
	if d.seen[channel] == nil {
		d.seen[channel] = &state{}
	}
	return d.seen[channel]
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
func (d *Door) Visit(here bool) { d.VisitAt(0, here) }

// VisitAt has a person walk up to the camera on a hub's channel, or leave.
func (d *Door) VisitAt(channel int, here bool) {
	d.mu.Lock()
	s := d.at(channel)
	s.person, s.motion = here, here
	d.mu.Unlock()
}

// Ring presses the doorbell button (true) or lets it go (false).
func (d *Door) Ring(pressed bool) {
	d.mu.Lock()
	d.at(0).doorbell = pressed
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
	raw, _ := io.ReadAll(r.Body)
	var body []struct {
		Param struct {
			Channel int `json:"channel"`
			User    struct {
				UserName string `json:"userName"`
				Password string `json:"password"`
			} `json:"User"`
		} `json:"param"`
	}
	_ = json.Unmarshal(raw, &body)
	channel := 0
	if len(body) > 0 {
		channel = body[0].Param.Channel
	}
	if q := r.URL.Query().Get("channel"); q != "" {
		channel, _ = strconv.Atoi(q)
	}
	if cmd == "Login" {
		if len(body) == 0 || body[0].Param.User.UserName != d.User || body[0].Param.User.Password != d.Password {
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
	// A channel with no camera answers as a hub's empty channel does.
	if channel != 0 && !slices.ContainsFunc(d.Cams, func(c Cam) bool { return c.Channel == channel && c.Online }) {
		fail(-17, "rcv failed")
		return
	}
	s := d.at(channel)
	switch cmd {
	case "GetChannelstatus":
		if len(d.Cams) == 0 {
			fail(-9, "not support")
			return
		}
		status := []map[string]any{{"channel": 0, "name": d.Name, "online": 1, "typeInfo": "BELL"}}
		for _, c := range d.Cams {
			status = append(status, map[string]any{"channel": c.Channel, "name": c.Name, "online": b(c.Online), "typeInfo": "TrackMix"})
		}
		reply(map[string]any{"count": len(status), "status": status})
	case "GetDevInfo":
		reply(map[string]any{"DevInfo": map[string]any{"name": d.Name, "model": d.Model, "type": "BELL"}})
	case "GetEvents":
		if d.Old {
			fail(-9, "not support")
			return
		}
		reply(map[string]any{
			"ai": map[string]any{
				"people":  map[string]any{"alarm_state": b(s.person), "support": 1},
				"vehicle": map[string]any{"alarm_state": b(s.vehicle), "support": 1},
				"dog_cat": map[string]any{"alarm_state": 0, "support": 0},
			},
			"md":      map[string]any{"alarm_state": b(s.motion), "support": 1},
			"visitor": map[string]any{"alarm_state": b(s.doorbell), "support": 1},
		})
	case "GetAiState":
		reply(map[string]any{"channel": 0, "people": map[string]any{"alarm_state": b(s.person), "support": 1},
			"vehicle": map[string]any{"alarm_state": b(s.vehicle), "support": 1}})
	case "GetMdState":
		reply(map[string]any{"state": b(s.motion)})
	case "Snap":
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(PictureOf(channel))
	default:
		fail(-9, "not support "+strings.ToLower(cmd))
	}
}
