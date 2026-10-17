package app

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/relayclient"
)

// fakeBilling is yeix.io's Toskar billing API; fakeStripeRelay is Toskar's
// relay's subscription API, with each Checkout Session's state.
type fakeBilling struct {
	mu       sync.Mutex
	checkout []map[string]string
	portal   []string
	next     int
}

type fakeStripeRelay struct {
	mu        sync.Mutex
	open      bool
	sessions  map[string]string // session → pending, paid, inactive
	exchanges int
	expires   time.Duration
}

func mintToken(route string, exp time.Time) string {
	payload, _ := json.Marshal(map[string]any{"v": 1, "route": route, "exp": exp.Unix()})
	return "rt1." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func newAnywhereApp(t *testing.T) (*App, *fakeBilling, *fakeStripeRelay) {
	t.Helper()
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	dir := t.TempDir()
	a, err := New(Options{DataDir: dir, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })

	b := &fakeBilling{}
	billing := http.NewServeMux()
	billing.HandleFunc("POST /api/toskar/anywhere/checkout", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		b.mu.Lock()
		b.checkout = append(b.checkout, body)
		b.next++
		id := "cs_test_" + string(rune('a'+b.next))
		b.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"url": "https://checkout.stripe.test/" + id, "session_id": id, "expires": time.Now().Add(24 * time.Hour).Unix()})
	})
	billing.HandleFunc("POST /api/toskar/anywhere/portal", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		b.mu.Lock()
		b.portal = append(b.portal, body["checkout_session"])
		b.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]string{"url": "https://billing.stripe.test/session"})
	})
	bsrv := httptest.NewServer(billing)
	t.Cleanup(bsrv.Close)

	f := &fakeStripeRelay{open: true, sessions: map[string]string{}, expires: 30 * 24 * time.Hour}
	relay := http.NewServeMux()
	relay.HandleFunc("GET /v1/subscriptions/availability", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]bool{"open": f.open})
	})
	relay.HandleFunc("POST /v1/subscriptions/stripe", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Route           string `json:"route"`
			CheckoutSession string `json:"checkout_session"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.exchanges++
		switch f.sessions[body.CheckoutSession] {
		case "paid":
			_ = json.NewEncoder(w).Encode(map[string]any{"token": mintToken(body.Route, time.Now().Add(f.expires)), "env": "sandbox"})
		case "pending":
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"error":{"code":"CHECKOUT_PENDING"}}`)
		default:
			w.WriteHeader(http.StatusPaymentRequired)
			_, _ = io.WriteString(w, `{"error":{"code":"SUBSCRIPTION_INACTIVE"}}`)
		}
	})
	rsrv := httptest.NewUnstartedServer(relay)
	rsrv.StartTLS()
	t.Cleanup(rsrv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(rsrv.Certificate())
	addr := rsrv.Listener.Addr().String()

	a.anywhere.billing = bsrv.URL
	a.anywhere.relayForTest = &relayclient.Client{Relay: "example.com", RootCAs: pool, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}}
	return a, b, f
}

func (f *fakeStripeRelay) set(session, state string) {
	f.mu.Lock()
	f.sessions[session] = state
	f.mu.Unlock()
}

// Buying access from anywhere in Settings (#639): a checkout opens, Toskar
// waits for the payment, then keeps the token, turns access from anywhere
// on, renews it, and notices when the subscription ends.
func TestAnywhereCheckout(t *testing.T) {
	a, b, relay := newAnywhereApp(t)
	ctx := context.Background()
	secrets := auth.NewSecretStore(a.Config.Get().DataDir)
	now := time.Now()
	a.anywhere.nowForTest = func() time.Time { return now }

	if s := a.subscription(); s == nil || s.State != "none" {
		t.Fatalf("before: %+v", s)
	}
	// Showing it asks the relay whether it takes subscriptions, in the
	// background; let that finish before the relay's answer changes.
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		a.anywhere.mu.Lock()
		asking := a.anywhere.asking
		a.anywhere.mu.Unlock()
		if !asking {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the relay's availability was never asked")
		}
	}
	if _, err := a.startCheckout(ctx, "weekly"); !errors.Is(err, errPlanInvalid) {
		t.Fatalf("weekly: %v", err)
	}
	// A full relay sells nothing; its answer is kept for a while.
	relay.mu.Lock()
	relay.open = false
	relay.mu.Unlock()
	a.anywhere.mu.Lock()
	a.anywhere.open = nil
	a.anywhere.mu.Unlock()
	if _, err := a.startCheckout(ctx, "monthly"); !errors.Is(err, errSoldOut) {
		t.Fatalf("sold out: %v", err)
	}
	relay.mu.Lock()
	relay.open = true
	relay.mu.Unlock()
	now = now.Add(availabilityFor + time.Second)

	url, err := a.startCheckout(ctx, "monthly")
	if err != nil || url != "https://checkout.stripe.test/cs_test_b" {
		t.Fatalf("checkout: %q %v", url, err)
	}
	route, _ := a.routeID()
	if got := b.checkout[len(b.checkout)-1]; got["route"] != route || got["plan"] != "monthly" {
		t.Fatalf("billing was asked for %v", got)
	}
	if s := a.subscription(); s.State != "pending" || s.Checkout != url {
		t.Fatalf("pending: %+v", s)
	}
	// Still paying: asked again in seconds.
	relay.set("cs_test_b", "pending")
	if next := a.anywhereTick(ctx); next != 5*time.Second || a.relayToken() != "" {
		t.Fatalf("while paying: next %v", next)
	}
	// Paid: the token is kept and access from anywhere turns on.
	relay.set("cs_test_b", "paid")
	a.anywhereTick(ctx)
	if a.relayToken() == "" || !a.Config.Get().RemoteAccess.Enabled {
		t.Fatal("no token, or access from anywhere still off, after paying")
	}
	if kept, _ := secrets.Read(stripeSessionName); kept != "cs_test_b" {
		t.Fatalf("the paid session isn't kept: %q", kept)
	}
	if open, _ := secrets.Read(stripeCheckoutName); open != "" {
		t.Fatal("the checkout is still open after paying")
	}
	if s := a.subscription(); s.State != "active" || s.Store != "stripe" || s.Expires.IsZero() {
		t.Fatalf("active: %+v", s)
	}
	if _, err := a.startCheckout(ctx, "yearly"); !errors.Is(err, errActive) {
		t.Fatalf("a second subscription: %v", err)
	}
	if url, err := a.manageSubscription(ctx); err != nil || url == "" || b.portal[0] != "cs_test_b" {
		t.Fatalf("manage: %q %v %v", url, err, b.portal)
	}

	// Far from its end, nothing is asked; near it, the token is renewed
	// without touching the switch.
	relay.mu.Lock()
	before := relay.exchanges
	relay.mu.Unlock()
	a.anywhereTick(ctx)
	if relay.exchanges != before {
		t.Fatal("asked the relay with a month left")
	}
	if err := a.applySettingsPatch(ctx, map[string]any{"remote_access_enabled": false}); err != nil {
		t.Fatal(err)
	}
	_ = secrets.Write(relayclient.TokenName, mintToken(route, time.Now().Add(2*24*time.Hour)))
	a.anywhereTick(ctx)
	if _, exp, _ := relayclient.ParseToken(a.relayToken()); time.Until(exp) < 20*24*time.Hour {
		t.Fatalf("not renewed: ends %v", exp)
	}
	if a.Config.Get().RemoteAccess.Enabled {
		t.Fatal("a renewal turned access from anywhere back on")
	}

	// Ended: said so, and asked again only daily.
	relay.set("cs_test_b", "inactive")
	_ = secrets.Delete(relayclient.TokenName)
	if next := a.anywhereTick(ctx); next != 24*time.Hour {
		t.Fatalf("after it ended: next %v", next)
	}
	if s := a.subscription(); s.State != "ended" {
		t.Fatalf("ended: %+v", s)
	}

	// A checkout left unpaid is forgotten, rather than shown as ended.
	_ = secrets.Delete(stripeSessionName)
	if _, err := a.startCheckout(ctx, "yearly"); err != nil {
		t.Fatal(err)
	}
	relay.set("cs_test_c", "inactive")
	a.anywhereTick(ctx)
	if open, _ := secrets.Read(stripeCheckoutName); open != "" {
		t.Fatal("an expired checkout is still kept")
	}
	if s := a.subscription(); s.State != "none" {
		t.Fatalf("after an unpaid checkout: %+v", s)
	}
	if _, err := a.manageSubscription(ctx); !errors.Is(err, errNoneHere) {
		t.Fatalf("manage with nothing bought here: %v", err)
	}

	// An organization's relay has no subscription.
	if err := a.applySettingsPatch(ctx, map[string]any{"remote_access_relay": "relay.example.com"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.startCheckout(ctx, "monthly"); !errors.Is(err, errOwnRelay) {
		t.Fatalf("own relay: %v", err)
	}
	if s := a.subscription(); s != nil {
		t.Fatalf("own relay shows %+v", s)
	}
}
