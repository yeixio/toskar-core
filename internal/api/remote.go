package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Access from anywhere (#456, docs/remote-access.md): a second listener for
// traffic from outside the home network. It serves the routes a phone uses
// and nothing else: only TLS, only phone keys, never this computer's or the
// local network's trust, no sign-in, no pairing, and no web pages.

type remoteKey struct{}

// remoteRequest reports a request that came in on the remote listener,
// with the device key it was let in with.
func remoteRequest(r *http.Request) bool { return remoteToken(r) != "" }

func remoteToken(r *http.Request) string {
	v, _ := r.Context().Value(remoteKey{}).(string)
	return v
}

// remoteFailures counts wrong keys on the remote listener by address: after
// twenty in ten minutes, that address waits.
var remoteFailures = auth.NewLimiter(20, 10*time.Minute)

// RemoteStatus is what the remote listener is doing.
type RemoteStatus struct {
	// Listening is the address it's listening on, or "".
	Listening string `json:"listening,omitempty"`
	// Error is why it isn't, such as a port another program has.
	Error string `json:"error,omitempty"`
}

type remoteListener struct {
	mu     sync.Mutex
	server *http.Server
	// relayed takes devices' connections from the relay tunnel (#456).
	relayed *mergedListener
	status  RemoteStatus
}

// remoteHandler marks every request as remote, keeps it to the API, and
// turns away an address that sent too many wrong keys.
func (s *Server) remoteHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/") {
			http.NotFound(w, r)
			return
		}
		ip := remoteIP(r)
		// A device through a relay that hides client addresses has none:
		// counting those together would let one stranger lock out every
		// relayed device, and a device key can't be guessed anyway.
		counted := ip != "0.0.0.0"
		if counted && remoteFailures.Blocked(ip) {
			writeErr(w, http.StatusTooManyRequests, "REMOTE_THROTTLED", "too many wrong keys from this address; try again later", nil)
			return
		}
		// Only a paired device's key gets any further, checked before
		// anything else reads the request, so every wrong one counts.
		token, err := auth.BearerToken(r)
		if err != nil || token == "" || s.deps.VerifyAPIKey == nil {
			if counted {
				remoteFailures.Fail(ip)
			}
			writeErr(w, http.StatusUnauthorized, "UNAUTHORIZED", "a paired device's key is required", nil)
			return
		}
		rec, err := s.deps.VerifyAPIKey(r.Context(), token)
		if err != nil {
			if counted {
				remoteFailures.Fail(ip)
			}
			writeErr(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid api key", nil)
			return
		}
		if rec.Kind != auth.KindDevice {
			writeErr(w, http.StatusForbidden, "REMOTE_DEVICES_ONLY", "from outside the home network, only paired devices can connect", nil)
			return
		}
		ctx := context.WithValue(r.Context(), remoteKey{}, token)
		s.router.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ServeRemote starts the remote listener on addr, with the API's
// certificate, replacing one already running. It returns once it's
// listening, or with why it couldn't.
func (s *Server) ServeRemote(addr string) error {
	s.StopRemote()
	s.listenMu.Lock()
	cfg := s.tlsConfig
	s.listenMu.Unlock()
	if cfg == nil {
		err := errors.New("the API has no certificate")
		s.remote.set(RemoteStatus{Error: err.Error()})
		return err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		s.remote.set(RemoteStatus{Error: err.Error()})
		return err
	}
	srv := &http.Server{
		Handler:           s.remoteHandler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	merged := newMergedListener(ln)
	s.remote.mu.Lock()
	s.remote.server = srv
	s.remote.relayed = merged
	s.remote.status = RemoteStatus{Listening: ln.Addr().String()}
	s.remote.mu.Unlock()
	go func() {
		if err := srv.Serve(tls.NewListener(merged, cfg)); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.remote.set(RemoteStatus{Error: err.Error()})
		}
	}()
	return nil
}

// StopRemote closes the remote listener and its connections at once.
func (s *Server) StopRemote() {
	s.remote.mu.Lock()
	srv := s.remote.server
	s.remote.server, s.remote.relayed = nil, nil
	s.remote.status = RemoteStatus{}
	s.remote.mu.Unlock()
	if srv != nil {
		_ = srv.Close()
	}
}

// ServeRelayed answers a device's connection that came through the relay
// as the remote listener answers its own: TLS with the API's certificate,
// paired devices' keys only. It reports false when the listener is off.
func (s *Server) ServeRelayed(conn net.Conn) bool {
	s.remote.mu.Lock()
	merged := s.remote.relayed
	s.remote.mu.Unlock()
	return merged != nil && merged.push(conn)
}

// TLSCertificate is the API's certificate, which signs the address record
// devices find this computer by.
func (s *Server) TLSCertificate() (tls.Certificate, bool) {
	s.listenMu.Lock()
	defer s.listenMu.Unlock()
	if s.tlsConfig == nil || len(s.tlsConfig.Certificates) == 0 {
		return tls.Certificate{}, false
	}
	return s.tlsConfig.Certificates[0], true
}

// mergedListener accepts from a TCP listener and from connections pushed
// to it, so relayed devices get the same server as direct ones.
type mergedListener struct {
	net.Listener
	conns chan net.Conn
	errs  chan error
	done  chan struct{}
	once  sync.Once
}

func newMergedListener(ln net.Listener) *mergedListener {
	m := &mergedListener{Listener: ln, conns: make(chan net.Conn), errs: make(chan error, 1), done: make(chan struct{})}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				m.errs <- err
				return
			}
			if !m.push(c) {
				c.Close()
				return
			}
		}
	}()
	return m
}

func (m *mergedListener) push(c net.Conn) bool {
	select {
	case m.conns <- c:
		return true
	case <-m.done:
		return false
	}
}

func (m *mergedListener) Accept() (net.Conn, error) {
	select {
	case c := <-m.conns:
		return c, nil
	case err := <-m.errs:
		return nil, err
	case <-m.done:
		return nil, net.ErrClosed
	}
}

func (m *mergedListener) Close() error {
	m.once.Do(func() { close(m.done) })
	return m.Listener.Close()
}

// RemoteListening is what the remote listener is doing.
func (s *Server) RemoteListening() RemoteStatus {
	s.remote.mu.Lock()
	defer s.remote.mu.Unlock()
	return s.remote.status
}

func (l *remoteListener) set(st RemoteStatus) {
	l.mu.Lock()
	l.status = st
	l.mu.Unlock()
}

// handleRemoteAccess is access from anywhere's setting and what its listener
// is doing (#456), for Admins.
func (s *Server) handleRemoteAccess(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"enabled": false}
	if s.deps.Config != nil {
		cfg := s.deps.Config.Get()
		out["enabled"] = cfg.RemoteAccess.Enabled
		out["port"] = cfg.RemotePort()
		if cfg.RemoteAccess.Address != "" {
			out["address"] = cfg.RemoteAccess.Address
		}
	}
	if s.deps.RemoteReach != nil {
		reach := s.deps.RemoteReach()
		out["port_mapping"] = reach.PortMapping
		if reach.Mapped != "" {
			out["mapped"], out["mapped_by"] = reach.Mapped, reach.Method
		}
		if reach.MapError != "" {
			out["map_error"] = reach.MapError
		}
		if reach.MapReason != "" {
			out["map_reason"] = reach.MapReason
		}
		if len(reach.IPv6) > 0 {
			out["ipv6"] = reach.IPv6
		}
		out["reachable"] = reach.Reachable
		if reach.Reason != "" {
			out["reason"] = reach.Reason
		}
		if reach.Relay != "" {
			relay := map[string]any{"name": reach.Relay, "state": reach.RelayState}
			if reach.RelayError != "" {
				relay["error"] = reach.RelayError
			}
			if reach.Route != "" {
				relay["route"] = reach.Route
			}
			out["relay"] = relay
		}
		if reach.Check != nil {
			out["check"] = reach.Check
		}
		if len(reach.Others) > 0 {
			out["others"] = reach.Others
		}
	}
	st := s.RemoteListening()
	if st.Listening != "" {
		out["listening"] = st.Listening
	}
	if st.Error != "" {
		out["error"] = st.Error
	}
	writeJSON(w, http.StatusOK, out)
}

// RemoteReach is how the internet reaches the remote listener (#456): the
// router port it opened, its IPv6 addresses, and in one word, how.
type RemoteReach struct {
	// PortMapping is whether Toskar asks the router to open a port.
	PortMapping bool
	// Mapped is the router's outside address and port, and Method how it
	// was opened: pcp, nat-pmp, or upnp.
	Mapped, Method string
	MapError       string
	// MapReason is why the router's port doesn't reach the internet, in a
	// word (#602): off (not asked), no_gateway, no_answer, refused,
	// port_taken, no_public_address (the router has no internet address of
	// its own, #617), or, though a port was opened, double_nat or carrier_nat.
	// Empty when the port reaches the internet, or while still asking.
	MapReason string
	IPv6      []string
	// Reachable is direct (a router port at a public address), ipv6,
	// manual (a port forwarded by hand), relay, or none, with Reason: off,
	// not_listening, carrier_nat, or no_port.
	Reachable, Reason string
	// Relay is the relay used, and RelayState how it's doing: no_token,
	// connecting, connected, or error, with RelayError its code.
	Relay, RelayState, RelayError string
	// Route is the start of this computer's route ID, as the relay's
	// dashboard shows it (#602).
	Route string
	// Check is the last outside check (#546), nil before the first.
	Check *ReachCheck
	// Others are other Toskars running on this computer (#602): two copies
	// each enroll on the relay, under their own routes.
	Others []OtherToskar
}

// OtherToskar is another Toskar running on this computer.
type OtherToskar struct {
	Name    string `json:"name"`
	PID     int    `json:"pid"`
	APIPort int    `json:"api_port"`
	DataDir string `json:"data_dir"`
	Version string `json:"version"`
	// Remote is set when it has access from anywhere on too.
	Remote bool `json:"remote,omitempty"`
}

// ReachCheck is the relay reaching this computer from the internet (#546):
// through its tunnel, and each direct way in, as a device away from home.
type ReachCheck struct {
	// At is when the last check that reached the relay ran.
	At     time.Time  `json:"at,omitzero"`
	Relay  *ReachWay  `json:"relay,omitempty"`
	Direct []ReachWay `json:"direct,omitempty"`
	// Tried is the last attempt, and Error its code when it didn't reach
	// the relay: CHECK_UNSUPPORTED, CHECK_TOO_SOON, NO_TOKEN,
	// RELAY_UNREACHABLE, or the relay's code. The last result stays.
	Tried time.Time `json:"tried,omitzero"`
	Error string    `json:"error,omitempty"`
}

// ReachWay is how one way in did.
type ReachWay struct {
	Address string `json:"address,omitempty"`
	OK      bool   `json:"ok"`
	// Reason is no_tunnel, not_public, unresolved, refused, timeout,
	// unreachable, tls, wrong_computer, or http.
	Reason string `json:"reason,omitempty"`
	MS     int64  `json:"ms,omitempty"`
	// Status is the HTTP status that answered, for http; Answered names
	// the other Toskar that answered, for wrong_computer, such as "Toskar
	// on linix-rig" (#602). Both come from relays from 0.3.4.
	Status   int    `json:"status,omitempty"`
	Answered string `json:"answered,omitempty"`
}

// handleRemoteCheck runs the outside check now and answers what
// GET /remote-access does (#546).
func (s *Server) handleRemoteCheck(w http.ResponseWriter, r *http.Request) {
	if s.deps.CheckReach == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "The outside check isn't available.", nil)
		return
	}
	if err := s.deps.CheckReach(r.Context()); err != nil {
		writeErrFrom(w, http.StatusConflict, "REMOTE_ACCESS_OFF", err)
		return
	}
	s.handleRemoteAccess(w, r)
}

// handleRelayToken takes the route token the app got from a store
// subscription for this computer (#456): the computer then registers and
// keeps its tunnel on Toskar's relay. A paired device may hand it over from
// anywhere; turning access from anywhere on with it needs an Admin.
func (s *Server) handleRelayToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token  string `json:"token"`
		Enable bool   `json:"enable"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "INVALID_JSON", "invalid JSON", nil)
		return
	}
	if body.Enable && !auth.PrincipalFrom(r.Context()).Person.Role.AtLeast(auth.RoleAdmin) {
		writeErr(w, http.StatusForbidden, "ROLE_REQUIRED", "turning on access from anywhere needs the admin role", map[string]any{"role": string(auth.RoleAdmin)})
		return
	}
	if s.deps.SetRelayToken == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Access from anywhere isn't available.", nil)
		return
	}
	state, err := s.deps.SetRelayToken(r.Context(), body.Token, body.Enable)
	if err != nil {
		if code, _ := contracts.ErrorCode(err); code != "" {
			writeErrFrom(w, http.StatusBadRequest, code, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, "RELAY_TOKEN_FAILED", "Couldn't keep the relay token.", map[string]any{"cause": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": state})
}

// handleRouteSecret gives a paired device the route secret, on the home
// network only, never through the remote listener: devices paired before
// access from anywhere get it here the first time they connect at home.
func (s *Server) handleRouteSecret(w http.ResponseWriter, r *http.Request) {
	if remoteRequest(r) || !auth.FromLocalNetwork(r) {
		writeErr(w, http.StatusForbidden, "ROUTE_NOT_LOCAL", "the route secret is given only on the home network", nil)
		return
	}
	if s.deps.RouteSecret == nil {
		writeErr(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "Access from anywhere isn't available.", nil)
		return
	}
	secret, route, err := s.deps.RouteSecret()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ROUTE_FAILED", err.Error(), nil)
		return
	}
	out := map[string]any{"route_secret": secret, "route_id": route}
	if s.deps.RelayName != nil {
		out["relay"] = s.deps.RelayName()
	}
	s.addRelayToken(out)
	writeJSON(w, http.StatusOK, out)
}

// addRelayToken puts this computer's token for Toskar's relay in a route
// answer, which only paired devices on the home network get, so every one
// of them can look the computer up away from home, not only the one that
// subscribed (#456). A computer without one answers without it.
func (s *Server) addRelayToken(out map[string]any) {
	if s.deps.RelayToken == nil {
		return
	}
	if tok := s.deps.RelayToken(); tok != "" {
		out["token"] = tok
	}
}
