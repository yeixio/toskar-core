package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// An API path no route takes answers with a coded error (#588), with or
// without the web UI, so a client on another version says so instead of
// reading the web UI's page as an answer.
func TestMissingAPIRouteIsCoded(t *testing.T) {
	web := fstest.MapFS{"index.html": {Data: []byte("<!doctype html>app")}}
	for _, tc := range []struct {
		name string
		deps Dependencies
	}{{"with the web UI", Dependencies{WebRoot: web}}, {"without it", Dependencies{}}} {
		t.Run(tc.name, func(t *testing.T) {
			srv := NewServer(tc.deps)
			do := func(method, path string) (int, contracts.ErrorBody) {
				rec := httptest.NewRecorder()
				srv.Handler().ServeHTTP(rec, localRequest(method, path, nil))
				var body contracts.APIError
				_ = json.Unmarshal(rec.Body.Bytes(), &body)
				return rec.Code, body.Error
			}
			if status, e := do(http.MethodGet, "/api/v1/no-such-route"); status != http.StatusNotFound || e.Code != "ROUTE_NOT_FOUND" {
				t.Fatalf("unknown route: %d %+v", status, e)
			}
			if status, e := do(http.MethodDelete, "/api/v1/version"); status != http.StatusMethodNotAllowed || e.Code != "METHOD_NOT_ALLOWED" {
				t.Fatalf("wrong method: %d %+v", status, e)
			}
		})
	}
	// The web UI's own routes still get its page.
	rec := httptest.NewRecorder()
	NewServer(Dependencies{WebRoot: web}).Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/settings", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "<!doctype html>app" {
		t.Fatalf("web route: %d %q", rec.Code, rec.Body)
	}
}
