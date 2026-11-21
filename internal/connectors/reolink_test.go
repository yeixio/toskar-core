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
