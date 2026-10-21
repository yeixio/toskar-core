package connectors

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Reolink reaches Reolink cameras, doorbells, and Home Hubs on the local
// network through Reolink's own reolink-cli (#667): its gateway keeps the
// camera sessions, and the CLI keeps the cameras' logins, so Toskar stores
// no camera password. Cameras give pictures from their main stream, their
// detections (people, visitor, motion, …) are devices to trigger on, and
// their speakers can be talked through.
type Reolink struct{}

func (Reolink) ID() string   { return "reolink" }
func (Reolink) Name() string { return "Reolink" }
func (Reolink) Description() string {
	return "See Reolink cameras and doorbells, and run automations when they see someone."
}
func (Reolink) Scopes() string {
	return "Toskar uses reolink-cli on this computer, which keeps each camera's login; Toskar never sees a camera password. " +
		"Install reolink-cli, add your cameras or Home Hub with `reolink-cli device add`, and give Toskar a Reolink user of its own if you can."
}

func (Reolink) Fields() []Field {
	return []Field{
		{Key: "command", Label: "reolink-cli", Optional: true, Placeholder: "~/.local/bin/reolink-cli",
			Help: "Where reolink-cli is; empty finds it on the PATH or in ~/.local/bin."},
		{Key: "gateway", Label: "Gateway address", Optional: true, Placeholder: reolinkDefaultGateway,
			Help: "The reolink-cli gateway on this computer; Toskar starts it if it isn't running."},
	}
}

const reolinkDefaultGateway = "127.0.0.1:9000"

// reolinkEvents are the detections a camera reports, as devices to
// trigger on: <camera>.<event>.
var reolinkEvents = []string{"people", "visitor", "motion", "vehicle", "package", "dog_cat"}

// reolinkAlias is a reolink-cli camera name.
var reolinkAlias = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// ValidReolinkDevice reports a camera (hub-ch0), or one of its detections
// (hub-ch0.people) when event is true.
func ValidReolinkDevice(device string, event bool) bool {
	alias, kind, dotted := strings.Cut(device, ".")
	if !reolinkAlias.MatchString(alias) {
		return false
	}
	if !event {
		return !dotted
	}
	for _, e := range reolinkEvents {
		if dotted && kind == e {
			return true
		}
	}
	return false
}

// reolinkCLI runs reolink-cli for a credential.
type reolinkCLI struct {
	command string
	gateway string
}

func newReolinkCLI(cred Credential) (reolinkCLI, error) {
	cmd := strings.TrimSpace(cred["command"])
	if strings.HasPrefix(cmd, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			cmd = filepath.Join(home, cmd[2:])
		}
	}
	if cmd == "" {
		if found, err := exec.LookPath("reolink-cli"); err == nil {
			cmd = found
		} else if home, err := os.UserHomeDir(); err == nil {
			cmd = filepath.Join(home, ".local", "bin", "reolink-cli")
		}
	}
	if _, err := os.Stat(cmd); err != nil {
		return reolinkCLI{}, errors.New("reolink-cli isn't installed here; install it from github.com/reolink/reolink-cli, or give its path")
	}
	gw := strings.TrimSpace(cred["gateway"])
	if gw == "" {
		gw = reolinkDefaultGateway
	}
	return reolinkCLI{command: cmd, gateway: gw}, nil
}

// reolinkReply is reolink-cli's JSON answer.
type reolinkReply struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (r reolinkCLI) cmd(ctx context.Context, args ...string) *exec.Cmd {
	c := exec.CommandContext(ctx, r.command, args...)
	c.Env = append(os.Environ(), "REOLINK_GATEWAY_ADDR="+r.gateway)
	return c
}

// run runs a command and decodes its data into out.
func (r reolinkCLI) run(ctx context.Context, out any, args ...string) error {
	if err := r.ensureGateway(ctx); err != nil {
		return err
	}
	raw, err := r.cmd(ctx, args...).Output()
	var reply reolinkReply
	if jerr := json.Unmarshal(raw, &reply); jerr != nil {
		if err != nil {
			return fmt.Errorf("reolink-cli %s: %w", args[len(args)-1], err)
		}
		return fmt.Errorf("reolink-cli answered something it shouldn't: %w", jerr)
	}
	if !reply.OK {
		if reply.Error != nil {
			return fmt.Errorf("reolink-cli: %s", reply.Error.Message)
		}
		return errors.New("reolink-cli failed")
	}
	if out != nil {
		return json.Unmarshal(reply.Data, out)
	}
	return nil
}

var (
	gatewayMu      sync.Mutex
	gatewayStarted = map[string]*exec.Cmd{}
)

// ensureGateway starts reolink-cli's gateway when it isn't answering, and
// keeps it running for Toskar's lifetime.
func (r reolinkCLI) ensureGateway(ctx context.Context) error {
	healthy := func() bool {
		hc := &http.Client{Timeout: 2 * time.Second}
		resp, err := hc.Get("http://" + r.gateway + "/api/health")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}
	if healthy() {
		return nil
	}
	gatewayMu.Lock()
	defer gatewayMu.Unlock()
	if healthy() {
		return nil
	}
	if old := gatewayStarted[r.gateway]; old != nil && old.ProcessState == nil {
		_ = old.Process.Kill()
	}
	// Not ctx: the gateway outlives the call that started it.
	c := r.cmd(context.Background(), "gateway", "start", "--addr", r.gateway)
	if err := c.Start(); err != nil {
		return fmt.Errorf("couldn't start the reolink-cli gateway: %w", err)
	}
	gatewayStarted[r.gateway] = c
	go func() { _ = c.Wait() }()
	for range 50 {
		if healthy() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("the reolink-cli gateway didn't start at %s", r.gateway)
}

// reolinkEntry is one of reolink-cli's registered cameras.
type reolinkEntry struct {
	Camera   string `json:"camera"`
	Host     string `json:"host"`
	Channel  *int   `json:"channel"`
	Disabled bool   `json:"disabled"`
}

// reolinkCamera is a camera Toskar can use, and the hub it's reached through.
type reolinkCamera struct {
	Alias   string
	Hub     string
	Channel int
	Name    string
	Model   string
}

var (
	reolinkInfoMu sync.Mutex
	reolinkInfo   = map[string]reolinkCamera{}
	reolinkInfoAt = map[string]time.Time{}
)

// cameras are the enabled cameras: a Home Hub's channels, which device
// expand registered as <hub>-ch<n>, and cameras of their own. A hub
// itself is how its channels are reached, not a camera.
func (r reolinkCLI) cameras(ctx context.Context) ([]reolinkCamera, error) {
	var list []reolinkEntry
	if err := r.run(ctx, &list, "device", "list"); err != nil {
		return nil, err
	}
	hubs := map[string]bool{}
	for _, e := range list {
		if e.Disabled || e.Channel == nil {
			continue
		}
		if hub, _, ok := strings.Cut(e.Camera, "-ch"); ok {
			hubs[hub] = true
		}
	}
	var out []reolinkCamera
	for _, e := range list {
		if e.Disabled || hubs[e.Camera] || !reolinkAlias.MatchString(e.Camera) {
			continue
		}
		cam := reolinkCamera{Alias: e.Camera, Hub: e.Camera}
		if e.Channel != nil {
			cam.Channel = *e.Channel
			if hub, _, ok := strings.Cut(e.Camera, "-ch"); ok && hubs[hub] {
				cam.Hub = hub
			}
		}
		cam.Name, cam.Model = r.describe(ctx, e.Camera)
		out = append(out, cam)
	}
	return out, nil
}

// describe is a camera's own name and model, read once in a while: asking
// a sleeping battery camera takes seconds.
func (r reolinkCLI) describe(ctx context.Context, alias string) (string, string) {
	reolinkInfoMu.Lock()
	if c, ok := reolinkInfo[alias]; ok && time.Since(reolinkInfoAt[alias]) < 10*time.Minute {
		reolinkInfoMu.Unlock()
		return c.Name, c.Model
	}
	reolinkInfoMu.Unlock()
	var info struct {
		Name    string `json:"name"`
		Model   string `json:"model"`
		Channel struct {
			Device struct {
				Name  string `json:"name"`
				Model string `json:"model"`
			} `json:"device"`
		} `json:"channel"`
	}
	name, model := alias, ""
	if err := r.run(ctx, &info, "--camera", alias, "info"); err == nil {
		if info.Channel.Device.Name != "" {
			name, model = info.Channel.Device.Name, info.Channel.Device.Model
		} else if info.Name != "" {
			name, model = info.Name, info.Model
		}
		reolinkInfoMu.Lock()
		reolinkInfo[alias] = reolinkCamera{Name: name, Model: model}
		reolinkInfoAt[alias] = time.Now()
		reolinkInfoMu.Unlock()
	}
	return name, model
}

// Check confirms reolink-cli works and has cameras.
func (Reolink) Check(ctx context.Context, _ *http.Client, cred Credential) (string, error) {
	r, err := newReolinkCLI(cred)
	if err != nil {
		return "", err
	}
	cams, err := r.cameras(ctx)
	if err != nil {
		return "", err
	}
	if len(cams) == 0 {
		return "", errors.New("reolink-cli has no cameras yet; add them with `reolink-cli device add`")
	}
	if len(cams) == 1 {
		return cams[0].Name, nil
	}
	return strconv.Itoa(len(cams)) + " cameras", nil
}

func (Reolink) Tools() []Tool { return nil }

// eventLabels name a camera's detections.
var eventLabels = map[string]string{
	"people": "Person", "visitor": "Doorbell press", "motion": "Motion",
	"vehicle": "Vehicle", "package": "Package", "dog_cat": "Pet",
}

// Devices are each camera, its speaker, and its detections.
func (Reolink) Devices(ctx context.Context, _ *http.Client, cred Credential) ([]Device, error) {
	r, err := newReolinkCLI(cred)
	if err != nil {
		return nil, err
	}
	cams, err := r.cameras(ctx)
	if err != nil {
		return nil, err
	}
	var out []Device
	for _, c := range cams {
		out = append(out, Device{Connector: "reolink", Device: c.Alias, Name: c.Name, Kind: KindCamera})
		for _, e := range reolinkEvents {
			// A doorbell press is a doorbell's.
			if e == "visitor" && !strings.Contains(strings.ToLower(c.Model), "doorbell") {
				continue
			}
			out = append(out, Device{Connector: "reolink", Device: c.Alias + "." + e, Name: c.Name + " " + eventLabels[e], Kind: KindSensor, State: "off"})
		}
	}
	return out, nil
}

// CameraPicture is a camera's picture from its main stream.
func (Reolink) CameraPicture(ctx context.Context, _ *http.Client, cred Credential, device string) ([]byte, string, error) {
	if !ValidReolinkDevice(device, false) {
		return nil, "", fmt.Errorf("%q isn't a Reolink camera", device)
	}
	r, err := newReolinkCLI(cred)
	if err != nil {
		return nil, "", err
	}
	f, err := os.CreateTemp("", "toskar-reolink-*.jpg")
	if err != nil {
		return nil, "", err
	}
	_ = f.Close()
	defer os.Remove(f.Name())
	if err := r.run(ctx, nil, "--camera", device, "snapshot", "--stream", "main", "--file", f.Name()); err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(f.Name())
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxCameraPicture {
		return nil, "", fmt.Errorf("the camera %s sent a picture over 10 MB", device)
	}
	return data, "image/jpeg", nil
}

// reolinkEvent is one line of reolink-cli's event stream.
type reolinkEvent struct {
	Channel   int    `json:"channel"`
	EventType string `json:"eventType"`
	Data      struct {
		Status  string   `json:"status"`
		AIType  string   `json:"aiType"`
		AITypes []string `json:"aiTypes"`
	} `json:"data"`
}

// active is what an event says is detected now: an ended event detects
// nothing.
func (e reolinkEvent) active() map[string]bool {
	out := map[string]bool{}
	if e.Data.Status == "" || strings.EqualFold(e.Data.Status, "none") {
		return out
	}
	for _, t := range append([]string{e.EventType, e.Data.AIType}, e.Data.AITypes...) {
		if t != "" && t != "none" && !strings.HasPrefix(t, "raw.") && !strings.HasPrefix(t, "siren.") {
			out[t] = true
		}
	}
	return out
}

// reolinkTracker turns events into on/off changes of detection devices.
type reolinkTracker struct {
	watched map[string]bool
	state   map[string]bool
}

// apply is the changes an event makes: a detection turning on, or one an
// event for the same camera no longer reports turning off.
func (t *reolinkTracker) apply(alias string, e reolinkEvent, at time.Time) []DeviceChange {
	if strings.HasPrefix(e.EventType, "raw.") || strings.HasPrefix(e.EventType, "siren.") {
		return nil
	}
	now := e.active()
	var out []DeviceChange
	for _, kind := range reolinkEvents {
		device := alias + "." + kind
		on := now[kind]
		if on == t.state[device] {
			continue
		}
		t.state[device] = on
		if !t.watched[device] {
			continue
		}
		from, to := "off", "on"
		if !on {
			from, to = "on", "off"
		}
		out = append(out, DeviceChange{Device: device, From: from, To: to, At: at})
	}
	return out
}

// WatchDevices follows each hub's (or camera's) event stream and reports
// its detections turning on and off.
func (Reolink) WatchDevices(ctx context.Context, cred Credential, devices []string, onChange func(DeviceChange)) error {
	r, err := newReolinkCLI(cred)
	if err != nil {
		return err
	}
	cams, err := r.cameras(ctx)
	if err != nil {
		return err
	}
	tracker := &reolinkTracker{watched: map[string]bool{}, state: map[string]bool{}}
	for _, d := range devices {
		tracker.watched[d] = true
	}
	// Each source's stream covers its cameras: a hub's covers its channels.
	byChannel := map[string]map[int]string{}
	for _, c := range cams {
		for _, e := range reolinkEvents {
			if tracker.watched[c.Alias+"."+e] {
				if byChannel[c.Hub] == nil {
					byChannel[c.Hub] = map[int]string{}
				}
				byChannel[c.Hub][c.Channel] = c.Alias
			}
		}
	}
	if len(byChannel) == 0 {
		return errors.New("none of the watched Reolink devices is a camera reolink-cli has")
	}
	if err := r.ensureGateway(ctx); err != nil {
		return err
	}
	var mu sync.Mutex
	errs := make(chan error, len(byChannel))
	for source, channels := range byChannel {
		go func(source string, channels map[int]string) {
			cmd := r.cmd(ctx, "--camera", source, "events", "stream", "--types", strings.Join(reolinkEvents, ","))
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			out, err := cmd.StdoutPipe()
			if err != nil {
				errs <- err
				return
			}
			if err := cmd.Start(); err != nil {
				errs <- err
				return
			}
			scan := bufio.NewScanner(out)
			scan.Buffer(make([]byte, 64<<10), 1<<20)
			for scan.Scan() {
				var e reolinkEvent
				if json.Unmarshal(scan.Bytes(), &e) != nil {
					continue
				}
				alias, ok := channels[e.Channel]
				if !ok {
					continue
				}
				mu.Lock()
				changes := tracker.apply(alias, e, time.Now())
				mu.Unlock()
				for _, c := range changes {
					onChange(c)
				}
			}
			_ = cmd.Wait()
			errs <- fmt.Errorf("the Reolink event stream for %s ended: %s", source, strings.TrimSpace(stderr.String()))
		}(source, channels)
	}
	select {
	case <-ctx.Done():
		return nil
	case err := <-errs:
		return err
	}
}
