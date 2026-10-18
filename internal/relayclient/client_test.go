package relayclient

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/rendezvous"
)

type memStore struct {
	mu sync.Mutex
	m  map[string]string
}

func (s *memStore) Read(name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[name]
	if !ok {
		return "", os.ErrNotExist
	}
	return v, nil
}

func (s *memStore) Write(name, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[name] = value
	return nil
}

func (s *memStore) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, name)
	return nil
}

func fakeToken(route string, exp time.Time) string {
	payload, _ := json.Marshal(map[string]any{"v": 1, "route": route, "exp": exp.Unix()})
	return "rt1." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

// fakeRelay speaks the relay's side of the protocol, enough to check the
// client against.
type fakeRelay struct {
	t       *testing.T
	srv     *httptest.Server
	secret  string
	mu      sync.Mutex
	tokens  map[string]bool
	record  []byte
	enrolls int
	opens   chan [2]string // stream, client
	// name and version are what the last tunnel sent.
	name    string
	version string
	streams chan net.Conn
	// checked is the last outside check asked for; noCheck answers as a
	// relay from before the check does.
	checked map[string]any
	noCheck bool
	// asked counts the calls that sent a token.
	asked int
}

func newFakeRelay(t *testing.T) *fakeRelay {
	f := &fakeRelay{t: t, secret: "enroll-secret-0123456789abcdef", tokens: map[string]bool{}, opens: make(chan [2]string, 4), streams: make(chan net.Conn, 4)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/enroll", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+f.secret {
			http.Error(w, `{"error":{"code":"ENROLL_DENIED"}}`, http.StatusUnauthorized)
			return
		}
		var body struct{ Route string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		tok := fakeToken(body.Route, time.Now().Add(30*24*time.Hour))
		f.mu.Lock()
		f.tokens[tok] = true
		f.enrolls++
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]string{"token": tok})
	})
	mux.HandleFunc("PUT /v1/routes/{route}/record", func(w http.ResponseWriter, r *http.Request) {
		if !f.authorized(r) {
			http.Error(w, `{"error":{"code":"TOKEN_EXPIRED"}}`, http.StatusUnauthorized)
			return
		}
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.record = b
		f.mu.Unlock()
		_, _ = io.WriteString(w, `{"ttl":1800}`)
	})
	mux.HandleFunc("GET /v1/tunnel", func(w http.ResponseWriter, r *http.Request) {
		if !f.authorized(r) {
			http.Error(w, `{"error":{"code":"TOKEN_EXPIRED"}}`, http.StatusUnauthorized)
			return
		}
		f.mu.Lock()
		f.name = r.Header.Get("Toskar-Computer-Name")
		f.version = r.Header.Get("Toskar-Version")
		f.mu.Unlock()
		fl := w.(http.Flusher)
		_, _ = io.WriteString(w, `{"type":"hello"}`+"\n")
		fl.Flush()
		for {
			select {
			case o := <-f.opens:
				fmt.Fprintf(w, `{"type":"open","stream":%q,"client":%q}`+"\n", o[0], o[1])
				fl.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
	mux.HandleFunc("POST /v1/tunnel/streams/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !f.authorized(r) || r.Header.Get("Upgrade") != "toskar-stream" || r.PathValue("id") != "s1" {
			http.Error(w, "no", http.StatusBadRequest)
			return
		}
		conn, buf, err := http.NewResponseController(w).Hijack()
		if err != nil {
			return
		}
		_, _ = buf.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: toskar-stream\r\n\r\n")
		_ = buf.Flush()
		f.streams <- conn
	})
	mux.HandleFunc("POST /v1/routes/{route}/check", func(w http.ResponseWriter, r *http.Request) {
		if !f.authorized(r) {
			http.Error(w, `{"error":{"code":"TOKEN_EXPIRED"}}`, http.StatusUnauthorized)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.noCheck {
			http.NotFound(w, r)
			return
		}
		if f.checked != nil {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"code":"CHECK_TOO_SOON"}}`)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&f.checked)
		_, _ = io.WriteString(w, `{"relay":{"ok":true,"ms":80},"direct":[{"address":"203.0.113.7:7333","ok":false,"reason":"timeout","ms":8000}]}`)
	})
	f.srv = httptest.NewUnstartedServer(mux)
	f.srv.StartTLS()
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeRelay) authorized(r *http.Request) bool {
	tok, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked++
	return f.tokens[tok]
}

func (f *fakeRelay) client(t *testing.T, enroll string, deliver func(net.Conn) bool) (*Client, tls.Certificate, *memStore) {
	cert := selfSigned(t)
	pool := x509.NewCertPool()
	pool.AddCert(f.srv.Certificate())
	secret := make([]byte, 32)
	_, _ = rand.Read(secret)
	store := &memStore{m: map[string]string{}}
	addr := f.srv.Listener.Addr().String()
	c := &Client{
		Relay:        "example.com",
		Secret:       secret,
		EnrollSecret: enroll,
		Store:        store,
		Certificate:  func() (tls.Certificate, bool) { return cert, true },
		Addresses:    func() []string { return []string{"203.0.113.7:7333"} },
		Deliver:      deliver,
		Name:         func() string { return "Mike's Mac" },
		Version:      "1.9.1",
		RootCAs:      pool,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}
	return c, cert, store
}

func selfSigned(t *testing.T) tls.Certificate {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "toskar"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestClient(t *testing.T) {
	f := newFakeRelay(t)
	delivered := make(chan net.Conn, 1)
	c, cert, store := f.client(t, f.secret, func(conn net.Conn) bool { delivered <- conn; return true })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	// It enrolls, keeps the token, and registers a record only a paired
	// device can open, listing the direct address and the relay's.
	waitFor(t, "a record", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.record != nil })
	if tok, _ := store.Read(TokenName); !strings.HasPrefix(tok, "rt1.") {
		t.Fatalf("token kept: %q", tok)
	}
	sum := sha256.Sum256(cert.Certificate[0])
	f.mu.Lock()
	blob := f.record
	f.mu.Unlock()
	opened, err := rendezvous.Open(blob, c.Secret, "sha256:"+hex.EncodeToString(sum[:]), time.Now(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"203.0.113.7:7333", c.Route() + ".example.com:443"}; !slices.Equal(opened.Addresses, want) {
		t.Fatalf("addresses: %v", opened.Addresses)
	}
	// The tunnel tells the relay the computer's name, for its dashboard (#602).
	waitFor(t, "the name", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.name != "" })
	f.mu.Lock()
	sent, version := f.name, f.version
	f.mu.Unlock()
	if name, _ := url.QueryUnescape(sent); name != "Mike's Mac" {
		t.Fatalf("name sent: %q", sent)
	}
	// And its version (toskar-relay#47).
	if version != "1.9.1" {
		t.Fatalf("version sent: %q", version)
	}
	// The tunnel and the record are separate, so either can come first.
	waitFor(t, "connected and registered", func() bool {
		st := c.Status()
		return st.State == "connected" && !st.Registered.IsZero()
	})
	if st := c.Status(); st.Registered.IsZero() || time.Until(st.Expires) < 29*24*time.Hour {
		t.Fatalf("status: %+v", st)
	}

	// A device through the relay reaches the listener with its own address.
	f.opens <- [2]string{"s1", "198.51.100.23"}
	var relaySide, computerSide net.Conn
	select {
	case relaySide = <-f.streams:
	case <-time.After(5 * time.Second):
		t.Fatal("no dial-back")
	}
	select {
	case computerSide = <-delivered:
	case <-time.After(5 * time.Second):
		t.Fatal("nothing delivered")
	}
	defer relaySide.Close()
	defer computerSide.Close()
	if host, _, _ := net.SplitHostPort(computerSide.RemoteAddr().String()); host != "198.51.100.23" {
		t.Fatalf("device address: %s", computerSide.RemoteAddr())
	}
	_, _ = io.WriteString(relaySide, "hello from the device\n")
	line, err := bufio.NewReader(computerSide).ReadString('\n')
	if err != nil || line != "hello from the device\n" {
		t.Fatalf("to the computer: %q %v", line, err)
	}
	_, _ = io.WriteString(computerSide, "hello back\n")
	line, err = bufio.NewReader(relaySide).ReadString('\n')
	if err != nil || line != "hello back\n" {
		t.Fatalf("to the device: %q %v", line, err)
	}
}

func TestToken(t *testing.T) {
	f := newFakeRelay(t)
	c, _, store := f.client(t, "", func(net.Conn) bool { return false })
	ctx := context.Background()
	if _, err := c.token(ctx, false); !errors.Is(err, errNoToken) {
		t.Fatalf("no token, no secret: %v", err)
	}

	// A kept token near its end is renewed by enrolling; a fresh one isn't.
	c.EnrollSecret = f.secret
	soon := fakeToken(c.Route(), time.Now().Add(24*time.Hour))
	_ = store.Write(TokenName, soon)
	tok, err := c.token(ctx, false)
	if err != nil || tok == soon || f.enrolls != 1 {
		t.Fatalf("renewal: %v %d", err, f.enrolls)
	}
	if again, _ := c.token(ctx, false); again != tok || f.enrolls != 1 {
		t.Fatal("renewed a fresh token")
	}

	// A wrong secret keeps a token that still works, and fails without one.
	c.EnrollSecret = "wrong-secret-0123456789abcdef"
	_ = store.Write(TokenName, soon)
	if kept, err := c.token(ctx, false); err != nil || kept != soon {
		t.Fatalf("kept on a failed renewal: %v", err)
	}
	_ = store.Delete(TokenName)
	if _, err := c.token(ctx, false); errorCode(err) != "ENROLL_DENIED" {
		t.Fatalf("no token, wrong secret: %v", err)
	}

	// Callers at once enroll once.
	c.EnrollSecret = f.secret
	_ = store.Delete(TokenName)
	before := f.enrolls
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = c.token(ctx, false) }()
	}
	wg.Wait()
	f.mu.Lock()
	n := f.enrolls - before
	f.mu.Unlock()
	if n != 1 {
		t.Fatalf("enrolled %d times at once", n)
	}

	if !tokenExpiry("nope").IsZero() || !tokenExpiry("rt1.!!.x").IsZero() {
		t.Fatal("expiry from garbage")
	}
}

// A computer with neither token nor secret says so and waits.
func TestNoToken(t *testing.T) {
	f := newFakeRelay(t)
	c, _, _ := f.client(t, "", func(net.Conn) bool { return false })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)
	waitFor(t, "no_token", func() bool { return c.Status().State == "no_token" })
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.record != nil {
		t.Fatal("registered without a token")
	}
}

// A token that ran out, with nothing to renew it (a lapsed subscription's),
// isn't sent: the computer says it ended, and a new token in the store
// takes over without a restart (#692).
func TestTokenRanOut(t *testing.T) {
	endedWait = 50 * time.Millisecond
	defer func() { endedWait = time.Minute }()
	f := newFakeRelay(t)
	c, _, store := f.client(t, "", func(net.Conn) bool { return false })
	exp := time.Now().Add(-time.Hour).Truncate(time.Second)
	_ = store.Write(TokenName, fakeToken(c.Route(), exp))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)
	waitFor(t, "ended", func() bool { return c.Status().State == "ended" })
	if st := c.Status(); !st.Expires.Equal(exp) || st.Error != "" {
		t.Fatalf("ended status: %+v", st)
	}
	time.Sleep(200 * time.Millisecond)
	f.mu.Lock()
	asked := f.asked
	f.mu.Unlock()
	if asked != 0 {
		t.Fatalf("sent the expired token %d times", asked)
	}
	if _, err := c.Check(ctx); CheckCode(err) != "TOKEN_EXPIRED" {
		t.Fatalf("check with the expired token: %v", err)
	}

	fresh := fakeToken(c.Route(), time.Now().Add(30*24*time.Hour))
	f.mu.Lock()
	f.tokens[fresh] = true
	f.mu.Unlock()
	_ = store.Write(TokenName, fresh)
	waitFor(t, "connected", func() bool { return c.Status().State == "connected" })
}

func TestStatusBeforeRun(t *testing.T) {
	if st := (&Client{}).Status(); st.State != "connecting" {
		t.Fatalf("before running: %q", st.State)
	}
}

func TestRouteAddress(t *testing.T) {
	c := &Client{Relay: "relay.example.com", Secret: make([]byte, 32)}
	if got := c.RouteAddress(); got != c.Route()+".relay.example.com:443" {
		t.Fatal(got)
	}
	c.Relay = "relay.localhost:7472"
	if got := c.RouteAddress(); got != c.Route()+".relay.localhost:7472" {
		t.Fatal(got)
	}
}

func TestParseToken(t *testing.T) {
	exp := time.Unix(1_800_000_000, 0)
	route, got, ok := ParseToken(" " + fakeToken("abc", exp) + "\n")
	if !ok || route != "abc" || !got.Equal(exp) {
		t.Fatalf("parse: %q %v %v", route, got, ok)
	}
	for _, bad := range []string{"", "rt1.", "rt1.e30", "rt1.e30.sig", "rt2." + strings.TrimPrefix(fakeToken("abc", exp), "rt1."), fakeToken("", exp)} {
		if _, _, ok := ParseToken(bad); ok {
			t.Errorf("parsed %q", bad)
		}
	}
}
