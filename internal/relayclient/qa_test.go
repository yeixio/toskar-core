package relayclient

// Regression tests for relay client bugs the Nov 2026 QA pass filed
// (layer 3 of the test plan: relay and core together). While an issue is
// open, its test is wrapped in qa.KnownBug, which skips while the bug is
// there and fails once it's fixed.

import (
	"context"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/qa"
)

// After the relay restarts, which forgets every record, the computer sends
// its record again as soon as its tunnel is back, not at the next
// five-minute registration.
func TestQARecordResentAfterRelayRestart(t *testing.T) {
	// Regression: toskar-core#1255
	f := newFakeRelay(t)
	c, _, _ := f.client(t, f.secret, func(net.Conn) bool { return false })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)
	waitFor(t, "connected and registered", func() bool {
		st := c.Status()
		f.mu.Lock()
		defer f.mu.Unlock()
		return st.State == "connected" && f.record != nil
	})
	// The relay restarts: records are gone and the tunnel drops.
	f.mu.Lock()
	f.record = nil
	f.mu.Unlock()
	f.srv.CloseClientConnections()
	reconnected, resent := false, false
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline) && !resent; time.Sleep(50 * time.Millisecond) {
		f.mu.Lock()
		resent = f.record != nil
		f.mu.Unlock()
		reconnected = reconnected || c.Status().State == "connected"
	}
	if !reconnected {
		t.Fatal("the tunnel never came back")
	}
	qa.KnownBug(t, "toskar-core#1255", func(t testing.TB) {
		if !resent {
			t.Error("the tunnel is back but the record wasn't sent again, so devices can't find the computer")
		}
	})
}

// A token the relay revoked (a refund, or the subscription moved to another
// computer) is treated like one that ended: not "error" with a raw code,
// and not retried until it would have run out.
func TestQARevokedTokenEndsInsteadOfRetrying(t *testing.T) {
	// Regression: toskar-core#1256
	var asked atomic.Int32
	revoked := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"code":"TOKEN_REVOKED"}}`)
	}))
	revoked.StartTLS()
	t.Cleanup(revoked.Close)
	f := newFakeRelay(t)
	c, _, store := f.client(t, "", func(net.Conn) bool { return false })
	pool := x509.NewCertPool()
	pool.AddCert(revoked.Certificate())
	addr := revoked.Listener.Addr().String()
	c.RootCAs = pool
	c.Dial = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	_ = store.Write(TokenName, fakeToken(c.Route(), time.Now().Add(20*24*time.Hour)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)
	waitFor(t, "the relay to refuse the token", func() bool { return asked.Load() >= 2 })
	time.Sleep(3 * time.Second)
	st := c.Status()
	tries := asked.Load()
	qa.KnownBug(t, "toskar-core#1256", func(t testing.TB) {
		if st.State != "ended" {
			t.Errorf("a revoked token leaves the relay %q (%s), want ended", st.State, st.Error)
		}
		// The tunnel and the record each ask once; more is retrying.
		if tries > 2 {
			t.Errorf("the revoked token was sent %d times in a few seconds", tries)
		}
	})
}
