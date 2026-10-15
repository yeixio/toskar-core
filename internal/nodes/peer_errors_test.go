package nodes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A computer that asks for JSON gets a failed request's stable code (#588);
// one from before codes gets the text it always has.
func TestPeerErrorsAreCodedForNewerComputers(t *testing.T) {
	srv, _, _, _ := setupInternalServer(t)
	ask := func(accept string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/internal/v1/models", nil)
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		rr := httptest.NewRecorder()
		srv.http.Handler.ServeHTTP(rr, req)
		return rr
	}
	if rr := ask(""); rr.Code != http.StatusUnauthorized || strings.TrimSpace(rr.Body.String()) != "authorization required" {
		t.Fatalf("older computer: %d %q", rr.Code, rr.Body)
	}
	rr := ask("application/json")
	err := responseError(rr.Code, rr.Body.Bytes(), true)
	if code, _ := contracts.ErrorCode(err); rr.Code != http.StatusUnauthorized || code != "PEER_UNAUTHORIZED" || err.Error() != "HTTP 401: authorization required" {
		t.Fatalf("newer computer: %d %q, read as %q (%s)", rr.Code, rr.Body, err, code)
	}
}

// The client reads a coded answer as a coded error, keeping the other
// computer's own code and values, and a text answer as before.
func TestResponseErrorReadsBothKinds(t *testing.T) {
	err := responseError(400, []byte(`{"error":{"code":"MODEL_NOT_INSTALLED","message":"model \"qwen3\" not installed","details":{"model_id":"qwen3"}}}`), true)
	code, details := contracts.ErrorCode(err)
	if code != "MODEL_NOT_INSTALLED" || details["model_id"] != "qwen3" || err.Error() != `HTTP 400: model "qwen3" not installed` {
		t.Fatalf("coded: %q %s %v", err, code, details)
	}
	err = responseError(400, []byte("model_id required\n"), false)
	if code, _ := contracts.ErrorCode(err); code != "" || err.Error() != "model_id required" {
		t.Fatalf("text: %q %s", err, code)
	}
	if err := responseError(502, nil, false); err.Error() != "HTTP 502" {
		t.Fatalf("empty: %q", err)
	}
}
