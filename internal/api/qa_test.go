package api

// Regression tests for the API bugs the Nov 2026 QA pass filed (layer 1 of
// the test plan: who may call what). Each checks what should happen; while
// its issue is open it is wrapped in qa.KnownBug, which skips while the bug
// is there and fails once it's fixed, so the fix unwraps it into a plain
// test.

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/qa"
	"github.com/yeixio/toskar-core/internal/store"
)

// qaDevices is a server with real API keys: pairing, companions and
// disconnecting work as they do in the app.
type qaDevices struct {
	srv  *Server
	keys *auth.APIKeyManager
}

func newQADevices(t *testing.T) *qaDevices {
	t.Helper()
	dir := t.TempDir()
	mgr, err := config.NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Update(func(c *config.Config) { c.APIHost, c.LANAPIEnabled = "0.0.0.0", true }); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	keys := auth.NewAPIKeyManager(db.SQL, auth.NewSecretStore(dir))
	srv := NewServer(Dependencies{
		Config:       mgr,
		VerifyAPIKey: keys.Verify,
		ListAPIKeys:  keys.List,
		RevokeAPIKey: keys.Revoke,
		Devices:      &auth.DevicePairer{CreateKey: keys.CreateDevice, CreateCompanion: keys.CreateCompanion, Companions: keys.Companions},
	})
	return &qaDevices{srv: srv, keys: keys}
}

// call is a request from another device on the home network with key, or
// from this computer without one.
func (q *qaDevices) call(method, path, key, body string) (int, string) {
	var r *http.Request
	if key == "" {
		r = localRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.RemoteAddr = "192.168.1.20:5000"
		r.Header.Set("Authorization", "Bearer "+key)
	}
	rec := httptest.NewRecorder()
	q.srv.Handler().ServeHTTP(rec, r)
	return rec.Code, rec.Body.String()
}

// Disconnecting a device disconnects what it connected, and a companion
// can't make more companions.
func TestQACompanionKeysGoWithTheirDevice(t *testing.T) {
	// Regression: toskar-core#1260
	q := newQADevices(t)
	phone, phoneKey, err := q.keys.CreateDevice(context.Background(), "QA phone")
	if err != nil {
		t.Fatal(err)
	}
	status, body := q.call(http.MethodPost, "/api/v1/me/devices", phoneKey, `{"device_name":"QA watch"}`)
	if status != http.StatusCreated {
		t.Fatalf("companion: %d %s", status, body)
	}
	var watch struct {
		APIKey string `json:"api_key"`
	}
	_ = json.Unmarshal([]byte(body), &watch)
	grandchild, _ := q.call(http.MethodPost, "/api/v1/me/devices", watch.APIKey, `{"device_name":"QA watch's watch"}`)
	// The owner disconnects the phone, as Devices → Disconnect does.
	if status, body := q.call(http.MethodDelete, "/api/v1/api-keys/"+phone.ID, "", ""); status != http.StatusNoContent {
		t.Fatalf("disconnect: %d %s", status, body)
	}
	if status, _ := q.call(http.MethodGet, "/api/v1/health", phoneKey, ""); status != http.StatusUnauthorized {
		t.Fatalf("the disconnected phone's key: %d", status)
	}
	after, _ := q.call(http.MethodGet, "/api/v1/health", watch.APIKey, "")
	qa.KnownBug(t, "toskar-core#1260", func(t testing.TB) {
		if after != http.StatusUnauthorized {
			t.Errorf("the phone's companion still works after the phone was disconnected: %d", after)
		}
		if grandchild == http.StatusCreated {
			t.Error("a companion made another companion")
		}
	})
}

// A stranger's wrong keys from a shared address don't lock out the paired
// devices behind it.
func TestQARemoteLockoutLetsValidDevicesIn(t *testing.T) {
	// Regression: toskar-core#1261
	dir := t.TempDir()
	mgr, err := config.NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := auth.APICertificate(auth.NewSecretStore(dir))
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Dependencies{
		Config: mgr,
		VerifyAPIKey: func(_ context.Context, secret string) (auth.APIKeyRecord, error) {
			if secret == "phone-key" {
				return auth.APIKeyRecord{ID: "d1", PersonID: auth.OwnerID, Kind: auth.KindDevice}, nil
			}
			return auth.APIKeyRecord{}, errors.New("invalid")
		},
	})
	srv.SetTLS(&tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, APITLS{Enabled: true})
	if err := srv.ServeRemote("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.StopRemote)
	remoteFailures.Forget("127.0.0.1")
	t.Cleanup(func() { remoteFailures.Forget("127.0.0.1") })
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	t.Cleanup(client.CloseIdleConnections)
	get := func(key string) int {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, "https://"+srv.RemoteListening().Listening+"/api/v1/health", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		return res.StatusCode
	}
	// A stranger on the same carrier address guesses twenty times.
	for range 20 {
		get("guess")
	}
	phone := get("phone-key")
	stranger := get("guess")
	qa.KnownBug(t, "toskar-core#1261", func(t testing.TB) {
		if phone != http.StatusOK {
			t.Errorf("a paired phone behind the same address: %d, want 200", phone)
		}
		if stranger != http.StatusTooManyRequests {
			t.Errorf("the stranger is no longer held back: %d", stranger)
		}
	})
}

// A paired device gets a token that can only look the computer up, never
// the computer's own, which could open its tunnel.
func TestQADevicesDontGetTheComputersRelayToken(t *testing.T) {
	// Regression: toskar-core#1258
	payload, _ := json.Marshal(map[string]any{"v": 1, "route": "route-1", "exp": time.Now().Add(30 * 24 * time.Hour).Unix()})
	computers := "rt1." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
	srv := NewServer(Dependencies{
		RouteSecret: func() (string, string, error) { return "secret", "route-1", nil },
		RelayToken:  func() string { return computers },
	})
	route, err := srv.route()
	if err != nil {
		t.Fatal(err)
	}
	qa.KnownBug(t, "toskar-core#1258", func(t testing.TB) {
		if route.Token == computers {
			t.Error("paired devices are handed the computer's own relay token, which has no device role")
		}
	})
}

// The public health check says the product's name.
func TestQAHealthSaysToskar(t *testing.T) {
	// Regression: toskar-core#1264
	srv := NewServer(Dependencies{})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/api/v1/health", nil))
	var health struct {
		Product string `json:"product"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &health)
	qa.KnownBug(t, "toskar-core#1264", func(t testing.TB) {
		if health.Product != "Toskar" {
			t.Errorf("/health says product %q, want Toskar", health.Product)
		}
	})
}
