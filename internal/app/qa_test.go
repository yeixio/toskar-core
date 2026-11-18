package app

// Regression tests for Access from anywhere bugs the Nov 2026 QA pass filed
// (layers 1 and 3 of the test plan). While an issue is open, its test is
// wrapped in qa.KnownBug, which skips while the bug is there and fails once
// it's fixed.

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/qa"
	"github.com/yeixio/toskar-core/internal/relayclient"
	"github.com/yeixio/toskar-core/internal/rendezvous"
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
	qa.KnownBug(t, "toskar-core#1259", func(t testing.TB) {
		for in, name := range got {
			if errs[in] == nil && name != "" {
				t.Errorf("%q is kept as an organization's relay %q", in, name)
			}
		}
	})
}

// A device's person can't knock the computer off the relay by handing it a
// worse token for the right route: one that lasts less than the one in use.
func TestQARelayTokenIsntReplacedByAWorseOne(t *testing.T) {
	// Regression: toskar-core#1258
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	dir := t.TempDir()
	a, err := New(Options{DataDir: dir, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })
	routeSecret, err := rendezvous.RouteSecret(auth.NewSecretStore(dir))
	if err != nil {
		t.Fatal(err)
	}
	route := rendezvous.RouteID(routeSecret)
	good := mintToken(route, time.Now().Add(30*24*time.Hour))
	if _, err := a.setRelayToken(context.Background(), good, false); err != nil {
		t.Fatal(err)
	}
	_, replaceErr := a.setRelayToken(context.Background(), mintToken(route, time.Now().Add(time.Minute)), false)
	kept := a.relayToken()
	qa.KnownBug(t, "toskar-core#1258", func(t testing.TB) {
		if replaceErr == nil || kept != good {
			t.Errorf("a token lasting a minute replaced the one lasting 30 days (err %v)", replaceErr)
		}
	})
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
	qa.KnownBug(t, "toskar-core#1257", func(t testing.TB) {
		if err == nil {
			t.Errorf("sold a second subscription for a covered computer: %s", url)
		}
	})
}
