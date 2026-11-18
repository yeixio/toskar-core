package push

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yeixio/toskar-core/internal/qa"
)

// A handle the relay can no longer open, or one sealed for another
// computer's key, is gone for good, like HANDLE_GONE: the registration is
// dropped so the app registers again.
func TestQAPermanentRefusalsAreGone(t *testing.T) {
	// Regression: toskar-core#1267
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	got := map[string]error{}
	for _, refusal := range []struct {
		status int
		code   string
	}{{http.StatusBadRequest, "BAD_HANDLE"}, {http.StatusForbidden, "NOT_YOUR_DEVICE"}} {
		relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(refusal.status)
			_, _ = io.WriteString(w, `{"error":{"code":"`+refusal.code+`"}}`)
		}))
		c := &Client{Relay: relay.URL, Key: priv}
		got[refusal.code] = c.Send(context.Background(), Message{Handle: "h1", Title: "x"})
		relay.Close()
	}
	qa.KnownBug(t, "toskar-core#1267", func(t testing.TB) {
		for code, err := range got {
			if !errors.Is(err, ErrGone) {
				t.Errorf("%s keeps the registration: %v", code, err)
			}
		}
	})
}
