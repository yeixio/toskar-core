package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/push"
	"github.com/yeixio/toskar-core/internal/store"
)

// A phone connects with the code the computer shows, from the local
// network and without a key, and its key reaches only what a phone uses
// (#216).
func TestConnectAPhone(t *testing.T) {
	dir := t.TempDir()
	mgr, err := config.NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Update(func(c *config.Config) { c.APIHost = "0.0.0.0" }); err != nil {
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
		Devices:      &auth.DevicePairer{CreateKey: keys.CreateDevice},
		PhoneAddress: func() (string, bool) { return "192.168.1.10:7331", true },
	})
	do := func(r *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, r)
		return rec
	}

	// The computer's own UI shows a code.
	start := do(localRequest(http.MethodPost, "/api/v1/devices/pairing", nil))
	if start.Code != http.StatusCreated {
		t.Fatalf("start: %d %s", start.Code, start.Body.String())
	}
	var shown struct {
		Code      string `json:"code"`
		Address   string `json:"address"`
		Reachable bool   `json:"reachable"`
	}
	_ = json.Unmarshal(start.Body.Bytes(), &shown)
	if len(shown.Code) != 6 || shown.Address != "192.168.1.10:7331" || !shown.Reachable {
		t.Fatalf("shown = %+v", shown)
	}

	// Nobody on the network can make a code without a key.
	remoteStart := httptest.NewRequest(http.MethodPost, "/api/v1/devices/pairing", nil)
	remoteStart.RemoteAddr = "192.168.1.20:5000"
	if got := do(remoteStart); got.Code != http.StatusUnauthorized {
		t.Fatalf("remote start without a key: %d", got.Code)
	}

	pair := func(addr, code string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/devices/pair", strings.NewReader(`{"code":"`+code+`","device_name":"Mike's iPhone"}`))
		r.RemoteAddr = addr
		return do(r)
	}
	if got := pair("8.8.8.8:5000", shown.Code); got.Code != http.StatusForbidden || !strings.Contains(got.Body.String(), "PAIRING_NOT_LOCAL") {
		t.Fatalf("from the internet: %d %s", got.Code, got.Body.String())
	}
	wrong := "000000"
	if shown.Code == wrong {
		wrong = "111111"
	}
	if got := pair("192.168.1.20:5000", wrong); got.Code != http.StatusUnauthorized || !strings.Contains(got.Body.String(), "PAIRING_WRONG_CODE") {
		t.Fatalf("wrong code: %d %s", got.Code, got.Body.String())
	}
	paired := pair("192.168.1.20:5000", shown.Code)
	if paired.Code != http.StatusCreated {
		t.Fatalf("pair: %d %s", paired.Code, paired.Body.String())
	}
	var got struct {
		APIKey string            `json:"api_key"`
		Key    auth.APIKeyRecord `json:"key"`
	}
	_ = json.Unmarshal(paired.Body.Bytes(), &got)
	if got.Key.Kind != auth.KindDevice || got.Key.Name != "Mike's iPhone" || got.APIKey == "" {
		t.Fatalf("paired = %+v", got)
	}

	// The computer sees who connected, without the code.
	status := do(localRequest(http.MethodGet, "/api/v1/devices/pairing", nil))
	if !strings.Contains(status.Body.String(), `"state":"connected"`) || !strings.Contains(status.Body.String(), "Mike's iPhone") || strings.Contains(status.Body.String(), shown.Code) {
		t.Fatalf("status = %s", status.Body.String())
	}

	phone := func(method, path string) int {
		r := httptest.NewRequest(method, path, nil)
		r.RemoteAddr = "192.168.1.20:5000"
		r.Header.Set("Authorization", "Bearer "+got.APIKey)
		return do(r).Code
	}
	if code := phone(http.MethodGet, "/api/v1/health"); code != http.StatusOK {
		t.Fatalf("phone health: %d", code)
	}
	for _, path := range []string{"/api/v1/api-keys", "/api/v1/devices/pairing"} {
		if code := phone(http.MethodGet, path); code != http.StatusForbidden {
			t.Fatalf("phone %s: %d", path, code)
		}
	}
	if code := phone(http.MethodPost, "/api/v1/devices/pairing"); code != http.StatusForbidden {
		t.Fatalf("a phone can't make codes: %d", code)
	}

	list, _ := keys.List(context.Background())
	if len(list) != 1 || list[0].Kind != auth.KindDevice {
		t.Fatalf("keys = %+v", list)
	}

	// The phone connects its watch, from anywhere, with no code: the watch
	// gets a key of its own (yeixio/toskar-apps#213).
	companion := func(addr, key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/me/devices", strings.NewReader(`{"device_name":"Watch with Mike's phone"}`))
		r.RemoteAddr = addr
		if key != "" {
			r.Header.Set("Authorization", "Bearer "+key)
		}
		return do(r)
	}
	if got := companion("192.168.1.20:5000", ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("companion without a key: %d", got.Code)
	}
	watch := companion("192.168.1.20:5000", got.APIKey)
	if watch.Code != http.StatusCreated {
		t.Fatalf("companion: %d %s", watch.Code, watch.Body.String())
	}
	var w struct {
		APIKey string            `json:"api_key"`
		Key    auth.APIKeyRecord `json:"key"`
	}
	_ = json.Unmarshal(watch.Body.Bytes(), &w)
	if w.APIKey == "" || w.APIKey == got.APIKey || w.Key.Kind != auth.KindDevice || w.Key.Name != "Watch with Mike's phone" || w.Key.PersonID != got.Key.PersonID {
		t.Fatalf("watch = %+v (phone %+v)", w.Key, got.Key)
	}
	r := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	r.RemoteAddr = "192.168.1.20:5000"
	r.Header.Set("Authorization", "Bearer "+w.APIKey)
	if code := do(r).Code; code != http.StatusOK {
		t.Fatalf("watch health: %d", code)
	}

	// No end of keys: past 20 devices, a companion is refused.
	for i := 0; i < 18; i++ {
		if got := companion("192.168.1.20:5000", got.APIKey); got.Code != http.StatusCreated {
			t.Fatalf("companion %d: %d", i, got.Code)
		}
	}
	if got := companion("192.168.1.20:5000", got.APIKey); got.Code != http.StatusConflict || !strings.Contains(got.Body.String(), "DEVICE_LIMIT") {
		t.Fatalf("21st device: %d %s", got.Code, got.Body.String())
	}
}

// A device removing this computer disconnects itself: its key, its watch's,
// and its notifications go, and other devices stay (#766).
func TestDeviceDisconnectsItself(t *testing.T) {
	dir := t.TempDir()
	mgr, err := config.NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = mgr.Update(func(c *config.Config) { c.APIHost = "0.0.0.0" })
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
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	regs := push.NewStore(db.SQL)
	srv.BindPush(regs, &push.Client{Key: priv})
	ctx := context.Background()
	_, phone, _ := keys.CreateDevice(ctx, "Sam's phone")
	_, tablet, _ := keys.CreateDevice(ctx, "Sam's tablet")
	_, script, _ := keys.Create(ctx, "a script")
	do := func(key, method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.RemoteAddr = "192.168.1.20:5000"
		r.Header.Set("Authorization", "Bearer "+key)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, r)
		return rec
	}
	var watch struct {
		APIKey string `json:"api_key"`
	}
	_ = json.Unmarshal(do(phone, http.MethodPost, "/api/v1/me/devices", `{"device_name":"Watch with Sam's phone"}`).Body.Bytes(), &watch)
	if watch.APIKey == "" {
		t.Fatal("no watch key")
	}
	for _, key := range []string{phone, tablet} {
		if got := do(key, http.MethodPut, "/api/v1/me/push", `{"handle":"h-`+key[:12]+`","platform":"ios"}`); got.Code != http.StatusOK {
			t.Fatalf("register: %d %s", got.Code, got.Body)
		}
	}

	if got := do(script, http.MethodDelete, "/api/v1/me/device", ""); got.Code != http.StatusBadRequest || !strings.Contains(got.Body.String(), "NOT_A_DEVICE") {
		t.Fatalf("a script key: %d %s", got.Code, got.Body)
	}
	if got := do(phone, http.MethodDelete, "/api/v1/me/device", ""); got.Code != http.StatusNoContent {
		t.Fatalf("disconnect: %d %s", got.Code, got.Body)
	}
	for name, key := range map[string]string{"phone": phone, "watch": watch.APIKey} {
		if got := do(key, http.MethodGet, "/api/v1/health", ""); got.Code != http.StatusUnauthorized {
			t.Fatalf("%s still connected: %d", name, got.Code)
		}
	}
	if got := do(tablet, http.MethodGet, "/api/v1/health", ""); got.Code != http.StatusOK {
		t.Fatalf("the tablet was disconnected too: %d", got.Code)
	}
	left, _ := regs.ForPerson(ctx, auth.OwnerID)
	if len(left) != 1 || left[0].Handle != "h-"+tablet[:12] {
		t.Fatalf("registrations left %+v", left)
	}
}
