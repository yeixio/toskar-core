package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/config"
)

// PUT /models/{id}/always-running keeps a model loaded, or lets it go
// (#966); a body without the choice is refused.
func TestSetModelAlwaysRunning(t *testing.T) {
	mgr, err := config.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var gotID string
	var gotOn *bool
	srv := NewServer(Dependencies{
		Config: mgr,
		SetModelAlwaysRunning: func(_ context.Context, id string, on bool) error {
			gotID, gotOn = id, &on
			return nil
		},
	})
	call := func(body string) (int, string) {
		r := httptest.NewRequest(http.MethodPut, "/api/v1/models/qwen3.5-4b-q4/always-running", strings.NewReader(body))
		r.RemoteAddr, r.Host = "127.0.0.1:50000", "127.0.0.1:7331"
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, r)
		return rec.Code, rec.Body.String()
	}
	if code, body := call(`{"always_running": true}`); code != http.StatusOK || gotID != "qwen3.5-4b-q4" || gotOn == nil || !*gotOn || !strings.Contains(body, `"always_running":true`) {
		t.Fatalf("on: %d %s, id %q", code, body, gotID)
	}
	if code, _ := call(`{"always_running": false}`); code != http.StatusOK || *gotOn {
		t.Fatalf("off: %d", code)
	}
	gotOn = nil
	if code, _ := call(`{}`); code != http.StatusBadRequest || gotOn != nil {
		t.Fatalf("no choice: %d", code)
	}
}
