package connectors

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Reolink is a Reolink camera or doorbell on this network, reached straight
// through its own HTTP API (#667): its picture now, and its person, vehicle,
// animal, motion, and doorbell events, with no Home Assistant or Reolink hub
// in between. One connection is one camera.
type Reolink struct{}

func (Reolink) ID() string   { return "reolink" }
func (Reolink) Name() string { return "Reolink camera" }
func (Reolink) Description() string {
	return "See who's at the door: a Reolink camera or doorbell's picture, and when it sees a person or someone rings."
}
func (Reolink) Scopes() string {
	return "Toskar signs in to the camera itself, on your network. In the Reolink app, add a user for Toskar under Settings › Advanced › User Management, " +
		"and turn on the camera's HTTP or HTTPS server under Settings › Network › Advanced › Server Settings. Nothing goes through Reolink's cloud."
}

func (Reolink) Fields() []Field {
	return []Field{
		{Key: "url", Label: "Camera address", Placeholder: "192.168.1.50",
			Help: "The camera's address on your network, from the Reolink app's Device Info. With HTTPS, Toskar remembers the camera's own certificate the first time it connects."},
		{Key: "username", Label: "User name"},
		{Key: "password", Label: "Password", Secret: true},
		{Key: "channel", Label: "Channel", Optional: true, Placeholder: "0",
			Help: "Leave empty for a camera on its own. A doorbell or battery camera paired to a Home Hub or NVR has no address of its own: use the hub's address and the camera's channel there, counted from 0."},
		{Key: "talk_camera", Label: "reolink-cli camera name", Optional: true, Advanced: true,
			Help: "Leave empty. To speak and listen through the camera, Toskar runs Reolink's reolink-cli with the address, channel and login above, so all it needs is reolink-cli installed on this computer. Give a name only to use a camera you added to reolink-cli yourself."},
		{Key: "talk_gateway", Label: "reolink-cli gateway", Optional: true, Advanced: true,
			Help: "Leave empty to use " + DefaultGateway + ". Toskar starts reolink-cli's gateway there when it isn't running, and does the same at another address on this computer, such as 127.0.0.1:9100. A gateway on another computer is left to whoever runs it."},
	}
}

func (Reolink) Tools() []Tool { return nil }

// Reolink's devices, as automations name them: the camera, and what it
// detects. Each detection is "on" while the camera sees it.
const (
	ReolinkCamera   = "camera"
	ReolinkPerson   = "person"
	ReolinkVehicle  = "vehicle"
	ReolinkAnimal   = "animal"
	ReolinkMotion   = "motion"
	ReolinkDoorbell = "doorbell"
)

// ReolinkDevices are the device names a Reolink connection has.
var ReolinkDevices = []string{ReolinkCamera, ReolinkPerson, ReolinkVehicle, ReolinkAnimal, ReolinkMotion, ReolinkDoorbell}

// reolinkPoll is how often a watch asks the camera what it sees. The
// camera's HTTP API has no push, and a second is quick enough for a visitor.
var reolinkPoll = time.Second

// reolinkChannel is the camera's channel: 0 for a camera on its own.
func reolinkChannel(cred Credential) int {
	n, err := strconv.Atoi(strings.TrimSpace(cred["channel"]))
	if err != nil || n < 0 || n > 255 {
		return 0
	}
	return n
}

func reolinkBase(cred Credential) (string, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(cred["url"]), "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errReolinkAddress
	}
	return u.Scheme + "://" + u.Host, nil
}

// reolinkUnreachable is a camera that didn't answer at its address.
type reolinkUnreachable struct{ err error }

func (e *reolinkUnreachable) Error() string { return e.err.Error() }
func (e *reolinkUnreachable) Unwrap() error { return e.err }

// Errors connecting a camera, said at the address field (#1345).
var (
	errReolinkAddress = contracts.NewError("CAMERA_ADDRESS", map[string]any{"field": "url"},
		errors.New("enter the camera's address, such as 192.168.1.50"))
	errReolinkNotThere = contracts.NewError("CAMERA_UNREACHABLE", map[string]any{"field": "url"},
		errors.New("couldn't reach a camera at that address. Check the address, and that the camera's HTTP or HTTPS server is on"))
)

// bareAddress is an address typed without http:// or https://, such as
// 192.168.1.50 from the Reolink app's Device Info (#1345).
func bareAddress(raw string) (string, bool) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" || strings.Contains(raw, "://") {
		return "", false
	}
	if u, err := url.Parse("http://" + raw); err != nil || u.Host == "" || u.Path != "" {
		return "", false
	}
	return raw, true
}

// reolinkHTTP is the client for a camera. Over HTTPS a Reolink camera has
// a certificate it made itself, which no authority signs, so the client
// trusts only the one it saw when the camera was connected.
func reolinkHTTP(c *http.Client, cred Credential) *http.Client {
	if !strings.HasPrefix(strings.ToLower(cred["url"]), "https://") {
		return c
	}
	pin := strings.ToLower(cred["fingerprint"])
	var base *http.Transport
	if t, ok := c.Transport.(*http.Transport); ok {
		base = t.Clone()
	} else {
		base = http.DefaultTransport.(*http.Transport).Clone()
	}
	base.TLSClientConfig = &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true, // checked against the pin below
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 {
				return errors.New("the camera sent no certificate")
			}
			if fingerprint(raw[0]) != pin {
				return errors.New("the camera's certificate changed since it was connected; connect it again in Settings if you replaced it")
			}
			return nil
		},
	}
	return &http.Client{Transport: base, Timeout: c.Timeout}
}

func fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}

// reolinkPin reads the certificate an HTTPS camera shows now.
func reolinkPin(ctx context.Context, base string) (string, error) {
	u, _ := url.Parse(base)
	host := u.Host
	if u.Port() == "" {
		host = net.JoinHostPort(u.Hostname(), "443")
	}
	d := &tls.Dialer{Config: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}} // read, then pinned
	conn, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		return "", &reolinkUnreachable{fmt.Errorf("could not reach the camera at %s: %w", u.Host, unwrapURLError(err))}
	}
	defer conn.Close()
	certs := conn.(*tls.Conn).ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return "", errors.New("the camera sent no certificate")
	}
	return fingerprint(certs[0].Raw), nil
}

func (r Reolink) Check(ctx context.Context, c *http.Client, cred Credential) (string, error) {
	// A bare address is tried over HTTPS, then HTTP, and kept as it answered.
	if addr, ok := bareAddress(cred["url"]); ok {
		var first error
		for _, scheme := range []string{"https://", "http://"} {
			cred["url"], cred["fingerprint"] = scheme+addr, ""
			account, err := r.check(ctx, c, cred)
			if err == nil {
				return account, nil
			}
			if first == nil {
				first = err
			}
			var gone *reolinkUnreachable
			if !errors.As(err, &gone) {
				return "", err
			}
		}
		cred["url"] = addr
		return "", reolinkCheckErr(first)
	}
	account, err := r.check(ctx, c, cred)
	return account, reolinkCheckErr(err)
}

// reolinkCheckErr says a camera that didn't answer plainly, at the address.
func reolinkCheckErr(err error) error {
	var gone *reolinkUnreachable
	if errors.As(err, &gone) {
		return errReolinkNotThere
	}
	return err
}

func (Reolink) check(ctx context.Context, c *http.Client, cred Credential) (string, error) {
	base, err := reolinkBase(cred)
	if err != nil {
		return "", err
	}
	if ch := strings.TrimSpace(cred["channel"]); ch != "" {
		if n, err := strconv.Atoi(ch); err != nil || n < 0 || n > 255 {
			return "", fmt.Errorf("the channel is a number from 0, such as 0 for a hub's first camera")
		}
	}
	// The first connect pins the certificate; later checks hold it to that.
	if strings.HasPrefix(base, "https://") && cred["fingerprint"] == "" {
		pin, err := reolinkPin(ctx, base)
		if err != nil {
			return "", err
		}
		cred["fingerprint"] = pin
	}
	reolinkForget(cred)
	name, model, err := reolinkInfo(ctx, c, cred)
	if err != nil {
		return "", err
	}
	switch {
	case name != "" && model != "":
		return name + " (" + model + ")", nil
	case model != "":
		return model, nil
	}
	return "Reolink camera", nil
}

// reolinkInfo is the camera's name, as set in the Reolink app, and model.
func reolinkInfo(ctx context.Context, c *http.Client, cred Credential) (name, model string, err error) {
	var info struct {
		DevInfo struct {
			Model string `json:"model"`
			Name  string `json:"name"`
		} `json:"DevInfo"`
	}
	if err := reolinkCmd(ctx, c, cred, "GetDevInfo", map[string]any{}, &info); err != nil {
		return "", "", err
	}
	return info.DevInfo.Name, info.DevInfo.Model, nil
}

// reolinkSession is a camera's sign-in, kept while it lasts: a camera
// allows only a few at once.
type reolinkSession struct {
	token   string
	expires time.Time
}

var (
	reolinkMu       sync.Mutex
	reolinkSessions = map[string]reolinkSession{}
)

func reolinkKey(cred Credential) string { return cred["url"] + "\x00" + cred["username"] }

func reolinkForget(cred Credential) {
	reolinkMu.Lock()
	delete(reolinkSessions, reolinkKey(cred))
	reolinkMu.Unlock()
}

// errReolinkLogin is a camera asking to sign in again.
var errReolinkLogin = errors.New("sign in again")

type reolinkResult struct {
	Cmd   string          `json:"cmd"`
	Code  int             `json:"code"`
	Value json.RawMessage `json:"value"`
	Error *struct {
		RspCode int    `json:"rspCode"`
		Detail  string `json:"detail"`
	} `json:"error"`
}

// reolinkPost sends one command and returns its result. The token goes in
// the address, as the camera wants it, so no error here names the address.
func reolinkPost(ctx context.Context, c *http.Client, base, token, cmd string, param any) (reolinkResult, error) {
	body, _ := json.Marshal([]map[string]any{{"cmd": cmd, "action": 0, "param": param}})
	q := url.Values{"cmd": {cmd}}
	if token != "" {
		q.Set("token", token)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/cgi-bin/api.cgi?"+q.Encode(), bytes.NewReader(body))
	if err != nil {
		return reolinkResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Toskar")
	resp, err := c.Do(req)
	if err != nil {
		return reolinkResult{}, &reolinkUnreachable{fmt.Errorf("could not reach the camera: %w", unwrapURLError(err))}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return reolinkResult{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return reolinkResult{}, fmt.Errorf("the camera answered %d", resp.StatusCode)
	}
	var out []reolinkResult
	if err := json.Unmarshal(raw, &out); err != nil || len(out) == 0 {
		return reolinkResult{}, errors.New("the camera's answer wasn't its API's; check the address and that its HTTP server is on")
	}
	r := out[0]
	if r.Code != 0 {
		detail, code := "", 0
		if r.Error != nil {
			detail, code = r.Error.Detail, r.Error.RspCode
		}
		if code == -6 || strings.Contains(strings.ToLower(detail), "login") {
			return r, errReolinkLogin
		}
		if code == -7 || strings.Contains(strings.ToLower(detail), "password") {
			return r, errors.New("the camera refused the user name or password")
		}
		if code == -9 || strings.Contains(strings.ToLower(detail), "not support") {
			return r, errReolinkUnsupported
		}
		return r, fmt.Errorf("the camera refused %s: %s", cmd, detail)
	}
	return r, nil
}

// errReolinkUnsupported is a command the camera doesn't have.
var errReolinkUnsupported = errors.New("the camera doesn't support that")

// reolinkToken signs in, or returns the sign-in that still lasts.
func reolinkToken(ctx context.Context, c *http.Client, cred Credential, base string) (string, error) {
	key := reolinkKey(cred)
	reolinkMu.Lock()
	s, ok := reolinkSessions[key]
	reolinkMu.Unlock()
	if ok && time.Now().Before(s.expires) {
		return s.token, nil
	}
	r, err := reolinkPost(ctx, c, base, "", "Login", map[string]any{
		"User": map[string]any{"Version": "0", "userName": cred["username"], "password": cred["password"]},
	})
	if errors.Is(err, errReolinkLogin) {
		return "", errors.New("the camera refused the user name or password")
	}
	if err != nil {
		return "", err
	}
	var v struct {
		Token struct {
			Name      string `json:"name"`
			LeaseTime int    `json:"leaseTime"`
		} `json:"Token"`
	}
	if json.Unmarshal(r.Value, &v) != nil || v.Token.Name == "" {
		return "", errors.New("the camera didn't sign Toskar in")
	}
	lease := time.Duration(v.Token.LeaseTime) * time.Second
	if lease <= 0 {
		lease = time.Hour
	}
	// Sign in again a minute before it runs out.
	reolinkMu.Lock()
	reolinkSessions[key] = reolinkSession{token: v.Token.Name, expires: time.Now().Add(max(lease-time.Minute, lease/2))}
	reolinkMu.Unlock()
	return v.Token.Name, nil
}

// reolinkCmd runs one command with a sign-in, signing in again once if the
// camera forgot it, and reads its value into out.
func reolinkCmd(ctx context.Context, c *http.Client, cred Credential, cmd string, param, out any) error {
	base, err := reolinkBase(cred)
	if err != nil {
		return err
	}
	c = reolinkHTTP(c, cred)
	for attempt := 0; ; attempt++ {
		token, err := reolinkToken(ctx, c, cred, base)
		if err != nil {
			return err
		}
		r, err := reolinkPost(ctx, c, base, token, cmd, param)
		if errors.Is(err, errReolinkLogin) && attempt == 0 {
			reolinkForget(cred)
			continue
		}
		if err != nil {
			return err
		}
		if out != nil && len(r.Value) > 0 {
			return json.Unmarshal(r.Value, out)
		}
		return nil
	}
}

// reolinkAlarm is one detection's state in the camera's answers.
type reolinkAlarm struct {
	AlarmState int  `json:"alarm_state"`
	Support    *int `json:"support"`
}

func (a *reolinkAlarm) state() string {
	if a != nil && a.AlarmState != 0 {
		return "on"
	}
	return "off"
}

func (a *reolinkAlarm) supported() bool {
	return a != nil && (a.Support == nil || *a.Support != 0)
}

type reolinkAI struct {
	People  *reolinkAlarm `json:"people"`
	Vehicle *reolinkAlarm `json:"vehicle"`
	DogCat  *reolinkAlarm `json:"dog_cat"`
}

// reolinkStates are what the camera sees now, by device, with only the
// detections it supports. Newer cameras answer GetEvents with them all;
// older ones, GetAiState and GetMdState.
func reolinkStates(ctx context.Context, c *http.Client, cred Credential) (map[string]string, error) {
	var ev struct {
		AI      *reolinkAI    `json:"ai"`
		MD      *reolinkAlarm `json:"md"`
		Visitor *reolinkAlarm `json:"visitor"`
	}
	err := reolinkCmd(ctx, c, cred, "GetEvents", map[string]any{"channel": reolinkChannel(cred)}, &ev)
	if errors.Is(err, errReolinkUnsupported) {
		var ai struct {
			reolinkAI
		}
		if err := reolinkCmd(ctx, c, cred, "GetAiState", map[string]any{"channel": reolinkChannel(cred)}, &ai); err != nil && !errors.Is(err, errReolinkUnsupported) {
			return nil, err
		}
		ev.AI = &ai.reolinkAI
		var md struct {
			State int `json:"state"`
		}
		if err := reolinkCmd(ctx, c, cred, "GetMdState", map[string]any{"channel": reolinkChannel(cred)}, &md); err != nil {
			return nil, err
		}
		ev.MD = &reolinkAlarm{AlarmState: md.State}
	} else if err != nil {
		return nil, err
	}
	out := map[string]string{}
	add := func(device string, a *reolinkAlarm) {
		if a.supported() {
			out[device] = a.state()
		}
	}
	if ev.AI != nil {
		add(ReolinkPerson, ev.AI.People)
		add(ReolinkVehicle, ev.AI.Vehicle)
		add(ReolinkAnimal, ev.AI.DogCat)
	}
	add(ReolinkMotion, ev.MD)
	add(ReolinkDoorbell, ev.Visitor)
	return out, nil
}

// reolinkWatch tells onChange of each change to the devices named, asking
// the camera every reolinkPoll, until ctx ends or the camera can't be
// reached; the caller connects again.
func reolinkWatch(ctx context.Context, c *http.Client, cred Credential, devices []string, onChange func(DeviceChange)) error {
	last, err := reolinkStates(ctx, c, cred)
	if err != nil {
		return err
	}
	tick := time.NewTicker(reolinkPoll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
		now, err := reolinkStates(ctx, c, cred)
		if err != nil {
			return err
		}
		for _, d := range devices {
			from, to := last[d], now[d]
			if to != "" && from != "" && from != to {
				onChange(DeviceChange{Device: d, From: from, To: to, At: time.Now()})
			}
		}
		last = now
	}
}

// reolinkSnapshot is the camera's picture now, as a JPEG.
func reolinkSnapshot(ctx context.Context, c *http.Client, cred Credential) ([]byte, string, error) {
	base, err := reolinkBase(cred)
	if err != nil {
		return nil, "", err
	}
	c = reolinkHTTP(c, cred)
	for attempt := 0; ; attempt++ {
		token, err := reolinkToken(ctx, c, cred, base)
		if err != nil {
			return nil, "", err
		}
		q := url.Values{"cmd": {"Snap"}, "channel": {strconv.Itoa(reolinkChannel(cred))}, "rs": {strconv.FormatInt(time.Now().UnixNano(), 36)}, "token": {token}}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/cgi-bin/api.cgi?"+q.Encode(), nil)
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("User-Agent", "Toskar")
		resp, err := c.Do(req)
		if err != nil {
			return nil, "", fmt.Errorf("could not reach the camera: %w", unwrapURLError(err))
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, maxCameraPicture+1))
		resp.Body.Close()
		if err != nil {
			return nil, "", err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, "", fmt.Errorf("the camera answered %d for its picture", resp.StatusCode)
		}
		if len(data) > maxCameraPicture {
			return nil, "", errors.New("the camera sent a picture over 10 MB")
		}
		// A refusal comes back as the API's JSON, not a picture.
		if !bytes.HasPrefix(data, []byte{0xff, 0xd8}) {
			var out []reolinkResult
			if json.Unmarshal(data, &out) == nil && len(out) > 0 && out[0].Error != nil &&
				(out[0].Error.RspCode == -6 || strings.Contains(strings.ToLower(out[0].Error.Detail), "login")) && attempt == 0 {
				reolinkForget(cred)
				continue
			}
			return nil, "", errors.New("the camera didn't send a picture")
		}
		return data, "image/jpeg", nil
	}
}

// reolinkDeviceList is a camera's devices for automations to pick from: the
// camera itself, and each detection it supports.
func reolinkDeviceList(ctx context.Context, c *http.Client, cred Credential) ([]Device, error) {
	states, err := reolinkStates(ctx, c, cred)
	if err != nil {
		return nil, err
	}
	name, _, _ := reolinkInfo(ctx, c, cred)
	if name == "" {
		name = "Reolink camera"
	}
	labels := map[string]string{ReolinkPerson: "person", ReolinkVehicle: "vehicle", ReolinkAnimal: "animal",
		ReolinkMotion: "motion", ReolinkDoorbell: "doorbell button"}
	out := []Device{{Connector: "reolink", Device: ReolinkCamera, Name: name, Kind: KindCamera, State: "on"}}
	for _, d := range ReolinkDevices[1:] {
		if st, ok := states[d]; ok {
			out = append(out, Device{Connector: "reolink", Device: d, Name: name + " " + labels[d], Kind: KindSensor, State: st})
		}
	}
	return out, nil
}
