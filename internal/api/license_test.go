package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/license"
)

// Toskar Pro's trial (yeixio/toskar-apps#336): only the desktop app on this
// computer sets the license, everyone can read it, and once the trial has
// ended new setup is refused with UNLOCK_REQUIRED.
func TestLicenseRoutes(t *testing.T) {
	mgr, err := config.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	keeper := license.Open(t.TempDir())
	srv := NewServer(Dependencies{Config: mgr})
	srv.BindLicense(keeper, "shell-secret")
	call := func(method, path, body, secret, from string) (int, string) {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.RemoteAddr, r.Host = from, "127.0.0.1:7331"
		if secret != "" {
			r.Header.Set(shellHeader, secret)
		}
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, r)
		return rec.Code, rec.Body.String()
	}
	const here = "127.0.0.1:50000"

	if code, body := call(http.MethodGet, "/api/v1/license", "", "", here); code != http.StatusOK || !strings.Contains(body, `"limited":false`) {
		t.Fatalf("no license: %d %s", code, body)
	}

	ends := time.Now().Add(9*24*time.Hour + time.Hour).UTC().Format(time.RFC3339)
	trial := `{"kind":"trial","trial_ends":"` + ends + `","store":"mac"}`
	for name, secret := range map[string]string{"no secret": "", "wrong secret": "guess"} {
		if code, body := call(http.MethodPut, "/api/v1/license", trial, secret, here); code != http.StatusForbidden || !strings.Contains(body, "SHELL_ONLY") {
			t.Errorf("%s: %d %s", name, code, body)
		}
	}
	code, body := call(http.MethodPut, "/api/v1/license", trial, "shell-secret", here)
	if code != http.StatusOK || !strings.Contains(body, `"days_left":10`) || !strings.Contains(body, `"kind":"trial"`) {
		t.Fatalf("set the trial: %d %s", code, body)
	}
	if code, body := call(http.MethodPut, "/api/v1/license", `{"kind":"forever"}`, "shell-secret", here); code != http.StatusBadRequest {
		t.Errorf("unknown kind: %d %s", code, body)
	}

	// During the trial new setup goes ahead (and fails later for its own
	// reasons, not the license).
	if _, body := call(http.MethodPost, "/api/v1/automations", `{}`, "", here); strings.Contains(body, "UNLOCK_REQUIRED") {
		t.Errorf("trial refused an automation: %s", body)
	}

	if code, body := call(http.MethodPut, "/api/v1/license", `{"kind":"ended","store":"mac"}`, "shell-secret", here); code != http.StatusOK || !strings.Contains(body, `"limited":true`) {
		t.Fatalf("end the trial: %d %s", code, body)
	}
	for _, path := range []string{"/api/v1/automations", "/api/v1/models/m1/install", "/api/v1/models/install-from-url", "/api/v1/models/import", "/api/v1/people"} {
		code, body := call(http.MethodPost, path, `{}`, "", here)
		if code != http.StatusForbidden || !strings.Contains(body, "UNLOCK_REQUIRED") || !strings.Contains(body, `"store":"mac"`) {
			t.Errorf("%s after the trial: %d %s", path, code, body)
		}
	}

	// A Mac trial not started yet: new setup waits for it.
	if code, body := call(http.MethodPut, "/api/v1/license", `{"kind":"available","store":"mac"}`, "shell-secret", here); code != http.StatusOK || !strings.Contains(body, `"limited":true`) {
		t.Fatalf("trial available: %d %s", code, body)
	}
	if code, body := call(http.MethodPost, "/api/v1/automations", `{}`, "", here); code != http.StatusForbidden || !strings.Contains(body, "TRIAL_NOT_STARTED") {
		t.Errorf("trial not started: %d %s", code, body)
	}

	if code, body := call(http.MethodPut, "/api/v1/license", `{"kind":"unlocked","store":"mac"}`, "shell-secret", here); code != http.StatusOK || !strings.Contains(body, `"limited":false`) {
		t.Fatalf("unlock: %d %s", code, body)
	}
	if _, body := call(http.MethodPost, "/api/v1/automations", `{}`, "", here); strings.Contains(body, "UNLOCK_REQUIRED") {
		t.Errorf("unlocked, still refused: %s", body)
	}
}

// Without the desktop app's secret, as for Core on its own, nothing sets it.
func TestLicenseNeedsTheShellSecret(t *testing.T) {
	mgr, err := config.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Dependencies{Config: mgr})
	srv.BindLicense(license.Open(t.TempDir()), "")
	r := httptest.NewRequest(http.MethodPut, "/api/v1/license", strings.NewReader(`{"kind":"ended"}`))
	r.RemoteAddr, r.Host = "127.0.0.1:50000", "127.0.0.1:7331"
	r.Header.Set(shellHeader, "")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("no secret configured: %d %s", rec.Code, rec.Body.String())
	}
}
