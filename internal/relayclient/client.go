// Package relayclient is this computer's side of a relay (#456,
// docs/remote-access.md): it gets a token by enrolling, keeps the sealed
// address record current on the rendezvous, and keeps a tunnel open so
// paired devices that can't reach the computer directly reach it through
// the relay. The relay protocol is toskar-relay's docs/protocol.md.
package relayclient

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/rendezvous"
)

// DefaultRelay is Toskar's relay. It's on port 4443 because it shares the
// website's load balancer, whose 443 ends TLS for the website (toskar-relay's
// deploy/README.md).
const DefaultRelay = "relay.toskar.ai:4443"

// Timing.
const (
	// RegisterEvery is how often the address record is sent; it lasts 30
	// minutes on the rendezvous.
	RegisterEvery = 5 * time.Minute
	// renewBefore is how long before a token runs out it's renewed.
	renewBefore = 7 * 24 * time.Hour
	// replacedWait is how long to stay off after another copy of this
	// computer took the route.
	replacedWait = time.Minute
)

// Store keeps the token between runs.
type Store interface {
	Read(name string) (string, error)
	Write(name, value string) error
	Delete(name string) error
}

// TokenName is the token's name in the store.
const TokenName = "relay-token"

// Client is one computer's connection to one relay.
type Client struct {
	// Relay is the relay's name, with a port when it isn't 443.
	Relay string
	// Secret is the route secret; the route ID and the record key come
	// from it.
	Secret []byte
	// EnrollSecret, when set, is traded for a token (a self-hosted relay).
	EnrollSecret string
	// Name is this computer's name, such as "linix-rig", sent when the
	// tunnel opens so the relay's operator can tell computers apart on its
	// dashboard (#602). It never reaches devices or the address record.
	Name func() string
	// Store keeps the token.
	Store Store
	// Certificate is the API certificate the record is signed with.
	Certificate func() (tls.Certificate, bool)
	// Addresses are the ways in that skip the relay, as host:port, best
	// first.
	Addresses func() []string
	// Deliver hands a relayed device's connection, with the device's
	// address, to the remote listener; false when it isn't listening.
	Deliver func(conn net.Conn) bool
	// RootCAs are the certificates the relay's is checked against; nil is
	// the system's.
	RootCAs *x509.CertPool
	// Dial connects to the relay; nil dials it directly.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
	Log  *slog.Logger

	mu     sync.Mutex
	status Status
	// tokenMu lets one caller at a time read or renew the token, so
	// registration and the tunnel starting together enroll once.
	tokenMu sync.Mutex
}

// Status is how the relay connection is doing, for Settings.
type Status struct {
	// State is no_token (nothing to connect with yet), connecting,
	// connected, or error.
	State string `json:"state"`
	// Error is the last failure's code, such as TOKEN_EXPIRED, or a short
	// message.
	Error string `json:"error,omitempty"`
	// Registered is when the address record was last sent.
	Registered time.Time `json:"registered,omitzero"`
	// Expires is when the token runs out.
	Expires time.Time `json:"expires,omitzero"`
}

// Status is the connection's state now.
func (c *Client) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.status
	if st.State == "" {
		// Started, and not yet heard from.
		st.State = "connecting"
	}
	return st
}

func (c *Client) setState(state, errText string) {
	c.mu.Lock()
	c.status.State, c.status.Error = state, errText
	c.mu.Unlock()
}

// Route is the route ID.
func (c *Client) Route() string { return rendezvous.RouteID(c.Secret) }

// RouteAddress is how devices reach this computer through the relay.
func (c *Client) RouteAddress() string {
	host, port := splitRelay(c.Relay)
	return net.JoinHostPort(c.Route()+"."+host, port)
}

func splitRelay(relay string) (host, port string) {
	if h, p, err := net.SplitHostPort(relay); err == nil {
		return h, p
	}
	return relay, "443"
}

func (c *Client) logger() *slog.Logger {
	if c.Log != nil {
		return c.Log
	}
	return slog.New(slog.DiscardHandler)
}

// Run keeps the record and the tunnel going until ctx ends.
func (c *Client) Run(ctx context.Context) {
	c.setState("connecting", "")
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); c.registerLoop(ctx) }()
	go func() { defer wg.Done(); c.tunnelLoop(ctx) }()
	wg.Wait()
}

// apiError is the relay's error answer.
type apiError struct {
	Status int
	Code   string
}

func (e *apiError) Error() string { return fmt.Sprintf("relay answered %d %s", e.Status, e.Code) }

func errorCode(err error) string {
	var ae *apiError
	if errors.As(err, &ae) && ae.Code != "" {
		return ae.Code
	}
	return "RELAY_UNREACHABLE"
}

var errNoToken = errors.New("no relay token")

// token is the token to use: the one kept, renewed by enrolling when it's
// near its end and an enrollment secret is set.
func (c *Client) token(ctx context.Context, renew bool) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	tok, _ := c.Store.Read(TokenName)
	tok = strings.TrimSpace(tok)
	exp := tokenExpiry(tok)
	if c.EnrollSecret != "" && (renew || tok == "" || time.Until(exp) < renewBefore) {
		fresh, err := c.enroll(ctx)
		if err == nil {
			tok, exp = fresh, tokenExpiry(fresh)
		} else if tok == "" || !time.Now().Before(exp) {
			return "", err
		}
	}
	if tok == "" {
		return "", errNoToken
	}
	c.mu.Lock()
	c.status.Expires = exp
	c.mu.Unlock()
	return tok, nil
}

// tokenExpiry reads a token's expiry without checking it; only the relay
// can check its own signature.
func tokenExpiry(tok string) time.Time {
	_, exp, _ := ParseToken(tok)
	return exp
}

// ParseToken reads a route token's route and expiry, without checking its
// signature: only the relay that signed it can. ok is false for anything
// that isn't a route token.
func ParseToken(tok string) (route string, exp time.Time, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(tok), "rt1.")
	if !found {
		return "", time.Time{}, false
	}
	payload, sig, found := strings.Cut(rest, ".")
	if !found || sig == "" {
		return "", time.Time{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return "", time.Time{}, false
	}
	var c struct {
		Route string `json:"route"`
		Exp   int64  `json:"exp"`
	}
	if json.Unmarshal(raw, &c) != nil || c.Exp == 0 || c.Route == "" {
		return "", time.Time{}, false
	}
	return c.Route, time.Unix(c.Exp, 0), true
}

func (c *Client) enroll(ctx context.Context) (string, error) {
	body, _ := json.Marshal(map[string]string{"route": c.Route()})
	resp, err := c.do(ctx, http.MethodPost, "/v1/enroll", c.EnrollSecret, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<10)).Decode(&out); err != nil || out.Token == "" {
		return "", errors.New("the relay's enrollment answer had no token")
	}
	if err := c.Store.Write(TokenName, out.Token); err != nil {
		return "", err
	}
	c.logger().Info("enrolled with the relay", "relay", c.Relay)
	return out.Token, nil
}

func (c *Client) httpClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   c.tlsConfig(),
			DialContext:       c.dialer(),
			ForceAttemptHTTP2: false,
		},
	}
}

func (c *Client) tlsConfig() *tls.Config {
	host, _ := splitRelay(c.Relay)
	return &tls.Config{ServerName: host, RootCAs: c.RootCAs, MinVersion: tls.VersionTLS12}
}

func (c *Client) dialer() func(ctx context.Context, network, addr string) (net.Conn, error) {
	if c.Dial != nil {
		return c.Dial
	}
	d := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	return d.DialContext
}

// do sends one API request and turns an error answer into an apiError.
func (c *Client) do(ctx context.Context, method, path, bearer string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, "https://"+c.Relay+path, body)
	if err != nil {
		return nil, err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		var e struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 8<<10)).Decode(&e)
		return nil, &apiError{Status: resp.StatusCode, Code: e.Error.Code}
	}
	return resp, nil
}

// register sends the sealed address record once.
func (c *Client) register(ctx context.Context) error {
	cert, ok := c.Certificate()
	if !ok {
		return errors.New("the API has no certificate")
	}
	tok, err := c.token(ctx, false)
	if err != nil {
		return err
	}
	addrs := append(c.Addresses(), c.RouteAddress())
	blob, err := rendezvous.Seal(c.Secret, cert, addrs, time.Now())
	if err != nil {
		return err
	}
	resp, err := c.do(ctx, http.MethodPut, "/v1/routes/"+c.Route()+"/record", tok, bytes.NewReader(blob))
	if isExpired(err) && c.EnrollSecret != "" {
		if tok, err = c.token(ctx, true); err == nil {
			resp, err = c.do(ctx, http.MethodPut, "/v1/routes/"+c.Route()+"/record", tok, bytes.NewReader(blob))
		}
	}
	if err != nil {
		return err
	}
	resp.Body.Close()
	c.mu.Lock()
	c.status.Registered = time.Now()
	c.mu.Unlock()
	return nil
}

func isExpired(err error) bool {
	var ae *apiError
	return errors.As(err, &ae) && (ae.Code == "TOKEN_EXPIRED" || ae.Code == "TOKEN_REVOKED" || ae.Code == "TOKEN_INVALID")
}

func (c *Client) registerLoop(ctx context.Context) {
	for {
		wait := RegisterEvery
		if err := c.register(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			if errors.Is(err, errNoToken) {
				c.setState("no_token", "")
			} else {
				c.logger().Warn("couldn't register with the relay", "relay", c.Relay, "err", err)
				wait = time.Minute
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// event is a line on the tunnel's control stream.
type event struct {
	Type   string `json:"type"`
	Stream string `json:"stream"`
	Client string `json:"client"`
	Reason string `json:"reason"`
}

func (c *Client) tunnelLoop(ctx context.Context) {
	backoff := 2 * time.Second
	for {
		started := time.Now()
		reason, err := c.tunnel(ctx)
		if ctx.Err() != nil {
			return
		}
		wait := backoff
		switch {
		case errors.Is(err, errNoToken):
			c.setState("no_token", "")
			wait = time.Minute
		case err != nil:
			c.setState("error", errorCode(err))
			c.logger().Warn("relay tunnel failed", "relay", c.Relay, "err", err)
		case reason == "REPLACED":
			c.setState("error", reason)
			wait = replacedWait
		default:
			c.setState("connecting", reason)
		}
		// A tunnel that stayed up a while starts the backoff over.
		if time.Since(started) > time.Minute {
			backoff = 2 * time.Second
		} else {
			backoff = min(backoff*2, 5*time.Minute)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// tunnel holds the control stream until it ends, answering each "open" by
// dialing back.
func (c *Client) tunnel(ctx context.Context) (string, error) {
	tok, err := c.token(ctx, false)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+c.Relay+"/v1/tunnel", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if c.Name != nil {
		if name := strings.TrimSpace(c.Name()); name != "" {
			req.Header.Set("Toskar-Computer-Name", url.QueryEscape(name))
		}
	}
	hc := c.httpClient()
	hc.Timeout = 0 // the stream stays open
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 8<<10)).Decode(&e)
		err := &apiError{Status: resp.StatusCode, Code: e.Error.Code}
		if isExpired(err) && c.EnrollSecret != "" {
			_, _ = c.token(ctx, true)
		}
		return "", err
	}
	// No line for three pings' time means the stream is gone.
	lines := make(chan []byte)
	scanErr := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			select {
			case lines <- append([]byte(nil), sc.Bytes()...):
			case <-ctx.Done():
				return
			}
		}
		scanErr <- sc.Err()
	}()
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case err := <-scanErr:
			if err == nil {
				err = io.EOF
			}
			return "", err
		case <-time.After(90 * time.Second):
			return "", errors.New("the relay stopped answering")
		case line := <-lines:
			var ev event
			if json.Unmarshal(line, &ev) != nil {
				continue
			}
			switch ev.Type {
			case "hello":
				c.setState("connected", "")
				c.logger().Info("relay tunnel open", "relay", c.Relay)
			case "open":
				go c.dialBack(ctx, tok, ev.Stream, ev.Client)
			case "end":
				if isExpired(&apiError{Code: ev.Reason}) && c.EnrollSecret != "" {
					_, _ = c.token(ctx, true)
				}
				return ev.Reason, nil
			}
		}
	}
}

// dialBack opens the stream the relay asked for and hands it to the remote
// listener as the device's connection.
func (c *Client) dialBack(ctx context.Context, tok, stream, client string) {
	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	host, port := splitRelay(c.Relay)
	raw, err := c.dialer()(dctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return
	}
	conn := tls.Client(raw, c.tlsConfig())
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	if err := conn.HandshakeContext(dctx); err != nil {
		raw.Close()
		return
	}
	head := "POST /v1/tunnel/streams/" + stream + " HTTP/1.1\r\nHost: " + host +
		"\r\nAuthorization: Bearer " + tok +
		"\r\nConnection: Upgrade\r\nUpgrade: toskar-stream\r\nContent-Length: 0\r\n\r\n"
	if _, err := io.WriteString(conn, head); err != nil {
		conn.Close()
		return
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil || resp.StatusCode != http.StatusSwitchingProtocols {
		conn.Close()
		return
	}
	_ = conn.SetDeadline(time.Time{})
	if !c.Deliver(&relayedConn{Conn: conn, r: br, remote: clientAddr(client)}) {
		conn.Close()
	}
}

// relayedConn is a device's connection through the relay: its bytes come
// from the stream, and its address is the device's, so the remote
// listener's limits count the device, not the relay.
type relayedConn struct {
	net.Conn
	r      *bufio.Reader
	remote net.Addr
}

func (c *relayedConn) Read(p []byte) (int, error) { return c.r.Read(p) }
func (c *relayedConn) RemoteAddr() net.Addr       { return c.remote }

func clientAddr(ip string) net.Addr {
	if a := net.ParseIP(ip); a != nil {
		return &net.TCPAddr{IP: a}
	}
	return &net.TCPAddr{IP: net.IPv4zero}
}
