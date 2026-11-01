package mixtls

import (
	"bufio"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// serve answers every request on a TLS-or-plain listener with its request
// line, and returns the listener's address and the client that trusts it.
func serve(t *testing.T) (string, *http.Client) {
	t.Helper()
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proto := "plain"
		if r.TLS != nil {
			proto = "tls"
		}
		_, _ = io.WriteString(w, proto+" "+r.Method+" "+r.URL.RequestURI())
	}))
	ts.StartTLS() // makes the certificate, and the client that trusts it
	config := ts.TLS
	client := ts.Client()
	ts.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	mixed := NewListener(ln, config)
	srv := &http.Server{Handler: ts.Config.Handler, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(mixed) }()
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().String(), client
}

func get(t *testing.T, client *http.Client, url string) string {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

func TestPlainAndTLSOnOnePort(t *testing.T) {
	addr, client := serve(t)
	// The plain request line arrives whole: the byte sniffing read isn't lost.
	if got := get(t, client, "http://"+addr+"/plain?x=1"); got != "plain GET /plain?x=1" {
		t.Fatalf("plain answer = %q", got)
	}
	if got := get(t, client, "https://"+addr+"/secure"); got != "tls GET /secure" {
		t.Fatalf("TLS answer = %q", got)
	}
}

func TestRawRequestLineIntact(t *testing.T) {
	addr, _ := serve(t)
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, _ = io.WriteString(c, "GET /whole HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n")
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "plain GET /whole" {
		t.Fatalf("answer = %q", body)
	}
}

// A client that connects and sends nothing doesn't hold up the next one.
func TestSilentClientDoesNotBlock(t *testing.T) {
	addr, client := serve(t)
	silent, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer silent.Close()
	done := make(chan string, 1)
	go func() { done <- get(t, client, "https://"+addr+"/after") }()
	select {
	case got := <-done:
		if !strings.HasPrefix(got, "tls GET /after") {
			t.Fatalf("answer = %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a silent client blocked the next one")
	}
}

func TestCloseUnblocksAccept(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	m := NewListener(ln, &tls.Config{})
	errc := make(chan error, 1)
	go func() {
		_, err := m.Accept()
		errc <- err
	}()
	time.Sleep(50 * time.Millisecond) // let Accept block
	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("Accept after Close returned no error")
		}
		if !errors.Is(err, net.ErrClosed) {
			t.Logf("Accept after Close: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Accept still blocked after Close")
	}
	_ = m.Close() // a second Close doesn't panic
}
