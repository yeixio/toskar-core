package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A good key that may not use MCP gets 403 and why, not a request for a
// key it already sent.
func TestRefusedKeyIsForbidden(t *testing.T) {
	for _, c := range []struct {
		err  error
		code int
		want string
	}{
		{&RefusedError{Message: "KEY_CHAT_ONLY: this key is for chat only"}, http.StatusForbidden, "KEY_CHAT_ONLY"},
		{errors.New("invalid api key"), http.StatusUnauthorized, "authorization required"},
	} {
		s := NewServer(Backend{Authorize: func(r *http.Request) (context.Context, error) { return nil, c.err }})
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}")))
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("%v: %d %s", c.err, rec.Code, rec.Body.String())
		}
	}
}
