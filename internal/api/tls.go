package api

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/mixtls"
)

// HTTPS for local network access (#213). The API answers HTTPS and plain
// HTTP on its one port: the desktop app and this computer's browser use
// plain HTTP over loopback, phones and other computers can use HTTPS, and
// a phone app that hasn't learned HTTPS yet still connects.

// APITLS is how the API speaks HTTPS.
type APITLS struct {
	Enabled bool `json:"enabled"`
	// Fingerprint is the certificate's SHA-256, and Short its start, such
	// as "4F2A-9C1B", for a person to compare with what a phone or browser
	// shows.
	Fingerprint string     `json:"fingerprint,omitempty"`
	Short       string     `json:"short,omitempty"`
	NotAfter    *time.Time `json:"not_after,omitempty"`
	// Custom is the person's own certificate (api_tls_cert, api_tls_key).
	Custom bool `json:"custom,omitempty"`
	// Error is why their certificate couldn't be used, so Toskar's own is.
	Error string `json:"error,omitempty"`
}

// SetTLS has the API answer HTTPS with config too, before it starts. Its
// one certificate can be replaced later with ReplaceTLSCertificate.
func (s *Server) SetTLS(config *tls.Config, info APITLS) {
	s.listenMu.Lock()
	defer s.listenMu.Unlock()
	if config != nil && config.GetCertificate == nil && len(config.Certificates) == 1 {
		s.tlsCert = config.Certificates[0]
		config.Certificates = nil
		config.GetCertificate = func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			s.listenMu.Lock()
			defer s.listenMu.Unlock()
			cert := s.tlsCert
			return &cert, nil
		}
	}
	s.tlsConfig, s.tlsInfo = config, info
}

// ReplaceTLSCertificate has every listener answer with cert from the next
// connection on, as when a certificate that names the computer is
// replaced (#1263).
func (s *Server) ReplaceTLSCertificate(cert tls.Certificate, info APITLS) {
	s.listenMu.Lock()
	defer s.listenMu.Unlock()
	s.tlsCert, s.tlsInfo = cert, info
}

// SetNextTLS says which certificate replaces the API's (#1263), and what to
// do when a device's key asks for it; "" when none will.
func (s *Server) SetNextTLS(fingerprint string, asked func(keyID string)) {
	s.listenMu.Lock()
	defer s.listenMu.Unlock()
	s.tlsNext, s.tlsNextAsked = fingerprint, asked
}

// nextTLSView is the certificate the API will switch to: paired devices
// save its fingerprint, so the switch doesn't look like another computer.
type nextTLSView struct {
	Fingerprint string `json:"fingerprint,omitempty"`
}

func (s *Server) handleNextTLS(w http.ResponseWriter, r *http.Request) {
	s.listenMu.Lock()
	next, asked := s.tlsNext, s.tlsNextAsked
	s.listenMu.Unlock()
	if p := auth.PrincipalFrom(r.Context()); next != "" && asked != nil && p.Via == auth.ViaAPIKey && p.KeyID != "" {
		asked(p.KeyID)
	}
	writeJSON(w, http.StatusOK, nextTLSView{Fingerprint: next})
}

// TLS is how the API speaks HTTPS.
func (s *Server) TLS() APITLS {
	s.listenMu.Lock()
	defer s.listenMu.Unlock()
	return s.tlsInfo
}

// withTLS serves HTTPS on ln as well as plain HTTP, once a certificate is set.
func (s *Server) withTLS(ln net.Listener) net.Listener {
	if s.tlsConfig == nil {
		return ln
	}
	return mixtls.NewListener(ln, s.tlsConfig)
}

func (s *Server) handleTLS(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.TLS())
}
