package app

// Regression tests for Access from anywhere bugs the Nov 2026 QA pass filed
// (layers 1 and 3 of the test plan).

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/relayclient"
)

// Typing Toskar's own relay under "Your organization's relay" means
// Toskar's relay, not an organization's on port 443.
func TestQAToskarsRelayTypedByHand(t *testing.T) {
	// Regression: toskar-core#1259
	got := map[string]string{}
	errs := map[string]error{}
	for _, in := range []string{"relay.toskar.ai", "relay.toskar.ai:4443", "https://relay.toskar.ai/"} {
		got[in], errs[in] = cleanRelayName(in)
	}
	for in, name := range got {
		if errs[in] != nil || name != "" {
			t.Errorf("%q is kept as an organization's relay %q (%v)", in, name, errs[in])
		}
	}
}

// A device's person can't knock the computer off the relay by handing it a
// worse token for the right route: one that lasts less than the one in use,
// or one the relay doesn't take.
func TestQARelayTokenIsntReplacedByAWorseOne(t *testing.T) {
	// Regression: toskar-core#1258
	a, _, _ := newAnywhereApp(t)
	route, err := a.routeID()
	if err != nil {
		t.Fatal(err)
	}
	good := mintToken(route, time.Now().Add(30*24*time.Hour))
	signed := map[string]bool{good: true}
	relay := http.NewServeMux()
	relay.HandleFunc("GET /v1/routes/{route}/usage", func(w http.ResponseWriter, r *http.Request) {
		if !signed[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")] {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"code":"TOKEN_INVALID"}}`)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"used": 0})
	})
	srv := httptest.NewUnstartedServer(relay)
	srv.StartTLS()
	t.Cleanup(srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	addr := srv.Listener.Addr().String()
	a.anywhere.relayForTest = &relayclient.Client{Relay: "example.com", RootCAs: pool, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}}
	ctx := context.Background()
	if _, err := a.setRelayTokenFromDevice(ctx, good, false); err != nil {
		t.Fatal(err)
	}
	// Shorter, though the relay signed it.
	short := mintToken(route, time.Now().Add(time.Minute))
	signed[short] = true
	if _, err := a.setRelayTokenFromDevice(ctx, short, false); !errors.Is(err, errRelayTokenWorse) {
		t.Errorf("a token lasting a minute replaced the one lasting 30 days: %v", err)
	}
	// Longer, but the relay didn't sign it.
	if _, err := a.setRelayTokenFromDevice(ctx, mintToken(route, time.Now().Add(60*24*time.Hour)), false); !errors.Is(err, errRelayToken) {
		t.Errorf("a token the relay refuses was taken: %v", err)
	}
	if kept := a.relayToken(); kept != good {
		t.Errorf("the token in use was replaced")
	}
	// Once the relay refuses the one in use, as after a move, a shorter
	// working one takes over.
	delete(signed, good)
	if _, err := a.setRelayTokenFromDevice(ctx, short, false); err != nil || a.relayToken() != short {
		t.Errorf("a working token didn't replace a revoked one: %v", err)
	}
}

// Settings asks the relay whether the computer is already covered, as the
// phone and the Mac app do, before it sells a subscription.
func TestQACheckoutAsksTheRelayForCoverage(t *testing.T) {
	// Regression: toskar-core#1257
	a, _, _ := newAnywhereApp(t)
	relay := http.NewServeMux()
	relay.HandleFunc("GET /v1/subscriptions/availability", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]bool{"open": true})
	})
	// A phone bought one, but its token hasn't reached this computer yet.
	relay.HandleFunc("GET /v1/routes/{route}/coverage", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"covered": true, "subscriptions": 1, "until": time.Now().Add(30 * 24 * time.Hour).Unix()})
	})
	srv := httptest.NewUnstartedServer(relay)
	srv.StartTLS()
	t.Cleanup(srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	addr := srv.Listener.Addr().String()
	a.anywhere.relayForTest = &relayclient.Client{Relay: "example.com", RootCAs: pool, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}}
	url, err := a.startCheckout(context.Background(), "monthly")
	if !errors.Is(err, errCovered) {
		t.Errorf("sold a second subscription for a covered computer: %s %v", url, err)
	}
}

// Access an operator granted by hand isn't shown as a subscription bought
// in the Toskar app.
func TestQAGrantIsntAnAppSubscription(t *testing.T) {
	// Regression: toskar-core#1256
	a, _, _ := newAnywhereApp(t)
	route, err := a.routeID()
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"v": 1, "route": route, "env": "grant", "exp": time.Now().Add(10 * 24 * time.Hour).Unix()})
	grant := "rt1." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
	if _, err := a.setRelayToken(context.Background(), grant, false); err != nil {
		t.Fatal(err)
	}
	if sub := a.subscription(); sub.State != "active" || sub.Store != "grant" {
		t.Errorf("a grant shows as %+v, want active from grant", sub)
	}
}
