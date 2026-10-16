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

// A paired phone registers its relay handle for itself, and only a paired
// device can (#460).
func TestPhoneRegistersForPush(t *testing.T) {
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
	srv := NewServer(Dependencies{Config: mgr, VerifyAPIKey: keys.Verify, ListAPIKeys: keys.List})
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	regs := push.NewStore(db.SQL)
	srv.BindPush(regs, &push.Client{Key: priv})
	ctx := context.Background()
	_, phoneKey, err := keys.CreateDevice(ctx, "Sam's phone")
	if err != nil {
		t.Fatal(err)
	}
	_, scriptKey, err := keys.Create(ctx, "a script")
	if err != nil {
		t.Fatal(err)
	}
	do := func(key, method, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/v1/me/push", strings.NewReader(body))
		r.RemoteAddr = "192.168.1.20:5000"
		r.Header.Set("Authorization", "Bearer "+key)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, r)
		return rec
	}
	var st PushStatus
	got := do(phoneKey, http.MethodGet, "")
	_ = json.Unmarshal(got.Body.Bytes(), &st)
	if got.Code != http.StatusOK || st.Registered || st.Relay != push.DefaultRelay || st.ComputerKey == "" {
		t.Fatalf("status before: %d %s", got.Code, got.Body)
	}
	if got := do(phoneKey, http.MethodPut, `{"handle":"sealed-h1","platform":"ios"}`); got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"registered":true`) {
		t.Fatalf("register: %d %s", got.Code, got.Body)
	}
	if regs, _ := regs.ForPerson(ctx, auth.OwnerID); len(regs) != 1 || regs[0].Handle != "sealed-h1" {
		t.Fatalf("registrations %+v", regs)
	}
	// The person's device list says which take pushes.
	list := localRequest(http.MethodGet, "/api/v1/me/devices", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, list)
	if !strings.Contains(rec.Body.String(), `"push":true`) {
		t.Fatalf("my devices: %s", rec.Body)
	}
	if got := do(phoneKey, http.MethodPut, `{"handle":"h","platform":"palm"}`); got.Code != http.StatusBadRequest || !strings.Contains(got.Body.String(), "PUSH_INVALID") {
		t.Fatalf("bad platform: %d %s", got.Code, got.Body)
	}
	if got := do(scriptKey, http.MethodPut, `{"handle":"h","platform":"ios"}`); got.Code != http.StatusForbidden || !strings.Contains(got.Body.String(), "PUSH_DEVICE_ONLY") {
		t.Fatalf("a script key registered: %d %s", got.Code, got.Body)
	}
	if got := do(phoneKey, http.MethodDelete, ""); got.Code != http.StatusNoContent {
		t.Fatalf("unregister: %d", got.Code)
	}
	if regs, _ := regs.ForPerson(ctx, auth.OwnerID); len(regs) != 0 {
		t.Fatalf("still registered: %+v", regs)
	}
}
