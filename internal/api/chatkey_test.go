package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/config"
)

// A key for chat only, as a CI job's (#455), asks and does nothing else:
// over the network, every other route refuses it.
func TestChatOnlyKeyOnlyAsks(t *testing.T) {
	mgr, err := config.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Update(func(c *config.Config) { c.APIHost = "0.0.0.0" }); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Dependencies{
		Config: mgr,
		VerifyAPIKey: func(ctx context.Context, got string) (auth.APIKeyRecord, error) {
			switch got {
			case "ci-key":
				return auth.APIKeyRecord{ID: "ci", Permissions: auth.CIAPIKeyPermissions()}, nil
			case "full-key":
				return auth.APIKeyRecord{ID: "full", Permissions: auth.DefaultAPIKeyPermissions()}, nil
			}
			return auth.APIKeyRecord{}, fmt.Errorf("invalid api key")
		},
	})
	call := func(method, path, key string) (int, string) {
		req := httptest.NewRequest(method, path, strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer "+key)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/health"},
		{http.MethodGet, "/api/v1/version"},
		{http.MethodGet, "/v1/models"},
		{http.MethodPost, "/v1/chat/completions"},
	} {
		if code, body := call(c.method, c.path, "ci-key"); code == http.StatusForbidden {
			t.Errorf("%s %s with a chat-only key: %d %s", c.method, c.path, code, body)
		}
	}
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/models"},
		{http.MethodPost, "/api/v1/chat"},
		{http.MethodGet, "/api/v1/conversations"},
		{http.MethodGet, "/api/v1/api-keys"},
		{http.MethodGet, "/api/v1/memory"},
	} {
		if code, body := call(c.method, c.path, "ci-key"); code != http.StatusForbidden || !strings.Contains(body, "KEY_CHAT_ONLY") {
			t.Errorf("%s %s with a chat-only key: %d %s", c.method, c.path, code, body)
		}
	}
	if code, body := call(http.MethodGet, "/api/v1/conversations", "full-key"); strings.Contains(body, "KEY_CHAT_ONLY") {
		t.Errorf("a key for everything was refused: %d %s", code, body)
	}
}

func TestCreateKeyWithItsPermissions(t *testing.T) {
	var set auth.APIKeyPermissions
	var revoked string
	failSet := false
	srv := NewServer(Dependencies{
		CreateAPIKey: func(ctx context.Context, name string) (auth.APIKeyRecord, string, error) {
			return auth.APIKeyRecord{ID: "k1", Name: name, Permissions: auth.DefaultAPIKeyPermissions()}, "ygg_secret", nil
		},
		SetAPIKeyPermissions: func(ctx context.Context, id string, p auth.APIKeyPermissions) (auth.APIKeyRecord, error) {
			if failSet {
				return auth.APIKeyRecord{}, errors.New("no")
			}
			set = p
			return auth.APIKeyRecord{ID: id, Permissions: p}, nil
		},
		RevokeAPIKey: func(ctx context.Context, id string) error { revoked = id; return nil },
	})
	create := func(body string) (int, string) {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, localRequest(http.MethodPost, "/api/v1/api-keys", strings.NewReader(body)))
		return rec.Code, rec.Body.String()
	}
	if code, body := create(`{"name":"GitHub Actions","preset":"ci"}`); code != http.StatusCreated || !strings.Contains(body, `"routes":"chat"`) || set != auth.CIAPIKeyPermissions() {
		t.Fatalf("ci preset: %d %s, set %+v", code, body, set)
	}
	if code, _ := create(`{"name":"x","permissions":{"memory":"never","knowledge":"never","tools":"read_only","placement":false}}`); code != http.StatusCreated || set.Tools != auth.ToolsReadOnly {
		t.Fatalf("permissions: %d, set %+v", code, set)
	}
	if code, _ := create(`{"name":"x","preset":"admin"}`); code != http.StatusBadRequest {
		t.Fatalf("unknown preset: %d", code)
	}
	if code, _ := create(`{"name":"x","permissions":{"memory":"sometimes","knowledge":"never","tools":"none"}}`); code != http.StatusBadRequest {
		t.Fatalf("invalid permissions: %d", code)
	}
	// A key whose permissions didn't take isn't left broader than asked.
	failSet = true
	if code, _ := create(`{"name":"x","preset":"ci"}`); code != http.StatusBadRequest || revoked != "k1" {
		t.Fatalf("failed permissions: %d, revoked %q", code, revoked)
	}
	// Without either, a key gets the defaults, as before.
	failSet, set = false, auth.APIKeyPermissions{}
	if code, _ := create(`{"name":"x"}`); code != http.StatusCreated || set != (auth.APIKeyPermissions{}) {
		t.Fatalf("plain key: %d, set %+v", code, set)
	}
}
