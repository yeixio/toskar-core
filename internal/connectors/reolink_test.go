package connectors

import (
	"bytes"
	"context"
	"net"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/connectors/reolinktest"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

func reolinkManager(t *testing.T) *Manager {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	m := NewManager(db.SQL, &memSecrets{m: map[string]string{}}, &memRegistry{tools: map[string]tools.Tool{}}, HomeAssistant{}, Reolink{})
	return m
}

// A Reolink doorbell connects straight to the camera, with no hub (#667):
// its name, its picture, and the detections it has.
func TestReolinkConnectsStraightToTheCamera(t *testing.T) {
	for _, secure := range []bool{false, true} {
		door := reolinktest.New(secure)
		m := reolinkManager(t)
		if secure {
			m.SetClient(door.Client())
		}
		ctx := context.Background()
		if _, err := m.Connect(ctx, "reolink", map[string]string{"url": door.URL, "username": "admin", "password": "wrong"}); err == nil || !strings.Contains(err.Error(), "user name or password") || strings.Contains(err.Error(), "wrong") {
			t.Fatalf("a wrong password: %v", err)
		}
		st, err := m.Connect(ctx, "reolink", map[string]string{"url": door.URL + "/", "username": "admin", "password": door.Password})
		if err != nil {
			t.Fatal(err)
		}
		if st.Account != "Front Door (Reolink Video Doorbell PoE)" {
			t.Fatalf("account = %q", st.Account)
		}
		pic, mime, err := m.CameraPicture(ctx, "reolink", "camera")
		if err != nil || mime != "image/jpeg" || !bytes.Equal(pic, reolinktest.Picture) {
			t.Fatalf("picture %q %v", mime, err)
		}
		devices, err := m.Devices(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, d := range devices {
			names = append(names, d.Device+"="+d.Name)
		}
		for _, want := range []string{"camera=Front Door", "person=Front Door person", "doorbell=Front Door doorbell button", "motion=Front Door motion"} {
			if !slices.Contains(names, want) {
				t.Fatalf("devices %v lack %s", names, want)
			}
		}
		if slices.ContainsFunc(names, func(s string) bool { return strings.HasPrefix(s, "animal=") }) {
			t.Fatalf("a detection the camera doesn't support was listed: %v", names)
		}
		door.Close()
	}
}

// Over HTTPS, the camera's own certificate is pinned when it's connected,
// and a different one is refused after (#667).
func TestReolinkPinsItsCertificate(t *testing.T) {
	door := reolinktest.New(true)
	defer door.Close()
	m := reolinkManager(t)
	m.SetClient(door.Client())
	ctx := context.Background()
	if _, err := m.Connect(ctx, "reolink", map[string]string{"url": door.URL, "username": "admin", "password": door.Password}); err != nil {
		t.Fatal(err)
	}
	cred, _ := m.credential("reolink")
	if len(cred["fingerprint"]) != 64 {
		t.Fatalf("no pin was kept: %q", cred["fingerprint"])
	}
	if _, _, err := reolinkSnapshot(ctx, m.client, cred); err != nil {
		t.Fatalf("the pinned camera: %v", err)
	}
	// Every httptest server has the same certificate, so a replaced
	// camera is a pin that no longer matches.
	cred["fingerprint"] = strings.Repeat("0", 64)
	reolinkForget(cred)
	if _, _, err := reolinkSnapshot(ctx, m.client, cred); err == nil || !strings.Contains(err.Error(), "certificate changed") {
		t.Fatalf("another camera's certificate: %v", err)
	}
}

// A watch tells of a person walking up and the doorbell ringing as they
// happen, signs in again after the camera restarts, and works with cameras
// that only have the older state commands (#667).
func TestReolinkWatchSeesVisitors(t *testing.T) {
	old := reolinkPoll
	reolinkPoll = 10 * time.Millisecond
	defer func() { reolinkPoll = old }()
	for _, older := range []bool{false, true} {
		door := reolinktest.New(false)
		door.Old = older
		m := reolinkManager(t)
		ctx, cancel := context.WithCancel(context.Background())
		if _, err := m.Connect(ctx, "reolink", map[string]string{"url": door.URL, "username": "admin", "password": door.Password}); err != nil {
			t.Fatal(err)
		}
		var mu sync.Mutex
		var seen []string
		done := make(chan error, 1)
		go func() {
			done <- m.WatchDevices(ctx, "reolink", []string{ReolinkPerson, ReolinkDoorbell}, func(c DeviceChange) {
				mu.Lock()
				seen = append(seen, c.Device+":"+c.From+">"+c.To)
				mu.Unlock()
			})
		}()
		waitFor := func(want string) {
			t.Helper()
			for range 300 {
				mu.Lock()
				ok := slices.Contains(seen, want)
				mu.Unlock()
				if ok {
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
			mu.Lock()
			defer mu.Unlock()
			t.Fatalf("older=%v: never saw %s, saw %v", older, want, seen)
		}
		time.Sleep(30 * time.Millisecond)
		door.Visit(true)
		waitFor("person:off>on")
		door.Forget()
		door.Visit(false)
		waitFor("person:on>off")
		if !older {
			door.Ring(true)
			waitFor("doorbell:off>on")
		}
		mu.Lock()
		for _, s := range seen {
			if strings.HasPrefix(s, "motion") {
				t.Errorf("reported a device nobody watched: %s", s)
			}
		}
		mu.Unlock()
		if door.Logins() < 2 {
			t.Errorf("didn't sign in again after the camera forgot: %d logins", door.Logins())
		}
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		door.Close()
	}
}

// The camera's address can be typed as the Reolink app shows it, with no
// http://; one that doesn't answer is said at the address field, in the
// app's language; the password isn't half shown back (#1345).
func TestReolinkBareAddressAndPlainErrors(t *testing.T) {
	// Regression: toskar-core#1345
	door := reolinktest.New(false)
	defer door.Close()
	m := reolinkManager(t)
	ctx := context.Background()
	bare := strings.TrimPrefix(door.URL, "http://")
	if _, err := m.Connect(ctx, "reolink", map[string]string{"url": bare, "username": "admin", "password": door.Password}); err != nil {
		t.Fatalf("a bare address: %v", err)
	}
	cred, _ := m.credential("reolink")
	if cred["url"] != door.URL {
		t.Fatalf("kept address %q, want %q", cred["url"], door.URL)
	}
	list, err := m.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range list {
		if st.ID == "reolink" && st.Values["password"] != "••••" {
			t.Fatalf("password shown as %q", st.Values["password"])
		}
	}
	for _, f := range (Reolink{}).Fields() {
		if strings.HasPrefix(f.Key, "talk_") && !f.Advanced {
			t.Fatalf("%s isn't folded under Advanced", f.Key)
		}
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gone := ln.Addr().String()
	ln.Close()
	for _, addr := range []string{gone, "http://" + gone} {
		_, err = m.Connect(ctx, "reolink", map[string]string{"url": addr, "username": "admin", "password": "secret-pw"})
		code, params := contracts.ErrorCode(err)
		if code != "CAMERA_UNREACHABLE" || params["field"] != "url" || strings.Contains(err.Error(), "secret-pw") {
			t.Fatalf("%s: unreachable camera: %q %v %v", addr, code, params, err)
		}
	}
	_, err = m.Connect(ctx, "reolink", map[string]string{"url": "ftp://cam", "username": "admin", "password": "x"})
	if code, _ := contracts.ErrorCode(err); code != "CAMERA_ADDRESS" {
		t.Fatalf("a wrong address: %q %v", code, err)
	}
}

// A doorbell on a Home Hub brings every camera paired to the hub, each by
// its own name and channel, with one sign-in between them (#1333): a
// person at the tracking camera starts only its own devices, and its
// picture and speaker are its own.
func TestReolinkHubHasEachCamera(t *testing.T) {
	old := reolinkPoll
	reolinkPoll = 10 * time.Millisecond
	defer func() { reolinkPoll = old }()
	door := reolinktest.New(false)
	defer door.Close()
	door.Cams = []reolinktest.Cam{{Channel: 4, Name: "Driveway 360", Online: true}, {Channel: 2, Name: "Garage", Online: false}}
	m := reolinkManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := m.Connect(ctx, "reolink", map[string]string{"url": door.URL, "username": "admin", "password": door.Password, "channel": "0"}); err != nil {
		t.Fatal(err)
	}
	devices, err := m.Devices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range devices {
		names = append(names, d.Device+"="+d.Name)
	}
	for _, want := range []string{"camera=Front Door", "doorbell=Front Door doorbell button", "camera@4=Driveway 360", "person@4=Driveway 360 person", "motion@4=Driveway 360 motion"} {
		if !slices.Contains(names, want) {
			t.Fatalf("devices %v lack %s", names, want)
		}
	}
	if slices.ContainsFunc(names, func(s string) bool { return strings.Contains(s, "@2") || strings.Contains(s, "@0") }) {
		t.Fatalf("an offline camera, or the door twice: %v", names)
	}
	pic, _, err := m.CameraPicture(ctx, "reolink", "camera@4")
	if err != nil || !bytes.Equal(pic, reolinktest.PictureOf(4)) {
		t.Fatalf("the tracking camera's picture: %v", err)
	}
	if pic, _, _ := m.CameraPicture(ctx, "reolink", "camera"); !bytes.Equal(pic, reolinktest.Picture) {
		t.Fatal("the door's picture isn't its own")
	}

	var mu sync.Mutex
	var seen []string
	done := make(chan error, 1)
	go func() {
		done <- m.WatchDevices(ctx, "reolink", []string{ReolinkPerson, ReolinkPerson + "@4"}, func(c DeviceChange) {
			mu.Lock()
			seen = append(seen, c.Device+":"+c.From+">"+c.To)
			mu.Unlock()
		})
	}()
	time.Sleep(40 * time.Millisecond)
	door.VisitAt(4, true)
	for range 300 {
		mu.Lock()
		ok := slices.Contains(seen, "person@4:off>on")
		mu.Unlock()
		if ok {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(seen, []string{"person@4:off>on"}) {
		t.Fatalf("saw %v, want only the tracking camera's person", seen)
	}
	if door.Logins() != 1 {
		t.Fatalf("%d sign-ins for one hub", door.Logins())
	}
}

// A device on another channel is that channel's camera, and the
// connection's own reolink-cli name doesn't follow it there (#1333).
func TestReolinkDeviceChannel(t *testing.T) {
	cred := Credential{"url": "https://192.168.68.56", "channel": "0", "talk_camera": "hub-ch0"}
	for _, tc := range []struct {
		device, base, channel, camera string
	}{
		{"camera", "camera", "0", "hub-ch0"},
		{"person@0", "person", "0", "hub-ch0"},
		{"camera@4", "camera", "4", ""},
		{"camera@x", "camera", "0", "hub-ch0"},
	} {
		got, base := reolinkDevice(cred, tc.device)
		if base != tc.base || got["channel"] != tc.channel || got["talk_camera"] != tc.camera {
			t.Errorf("%s: %s %v", tc.device, base, got)
		}
	}
	if cred["channel"] != "0" || cred["talk_camera"] != "hub-ch0" {
		t.Fatalf("the connection itself changed: %v", cred)
	}
	got, _ := reolinkDevice(cred, "camera@4")
	target, err := targetOf(got)
	if err != nil || !slices.Contains(target.args, "--channel=4") || slices.Contains(target.args, "--camera") {
		t.Fatalf("talking through channel 4: %v %v", target.args, err)
	}
}
