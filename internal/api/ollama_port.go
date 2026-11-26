package api

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"time"
)

// OllamaPort is Ollama's own port. Some apps that speak Ollama's API assume
// it and don't let you change it (#1396).
const OllamaPort = 11434

// ollamaPortListener is the optional listener on Ollama's port: off unless
// turned on, on this computer only, and serving only Ollama's routes.
type ollamaPortListener struct {
	mu     sync.Mutex
	server *http.Server
	addr   string
}

// ollamaPath reports the routes the Ollama port answers: Ollama's API, which
// ollamaRoutes serves on the API's own address too.
func ollamaPath(p string) bool {
	return strings.HasPrefix(p, "/api/") && ollamaRoute[strings.TrimPrefix(p, "/api/")]
}

var ollamaRoute = map[string]bool{
	"tags": true, "ps": true, "version": true, "chat": true, "generate": true, "show": true,
	"embed": true, "embeddings": true, "pull": true, "push": true, "create": true, "copy": true, "delete": true,
}

// ollamaPortHandler answers Ollama's routes through the API's own router,
// so keys, Auto, and profiles work as they do on the API's address. The
// root answers as Ollama's does, since apps check it to see a server is
// there; anything else is Ollama's 404.
func (s *Server) ollamaPortHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte("Ollama is running"))
			}
		case ollamaPath(r.URL.Path):
			s.router.ServeHTTP(w, r)
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"404 page not found"}`))
		}
	})
}

// ErrOllamaPortInUse is a listener that couldn't start because another
// program, usually Ollama itself, has the port.
var ErrOllamaPortInUse = errors.New("another program is using the port")

// ServeOllamaPort starts the listener on addr, replacing one already
// running. It never takes the port from another program: if the port is
// in use it answers ErrOllamaPortInUse and listens on nothing.
func (s *Server) ServeOllamaPort(addr string) error {
	s.StopOllamaPort()
	// Something already answering there, such as Ollama listening on every
	// address, keeps the port: on macOS a listener on 127.0.0.1 could
	// otherwise be let in beside one on 0.0.0.0, and take its local calls.
	if _, port, _ := net.SplitHostPort(addr); port != "0" {
		if c, err := net.DialTimeout("tcp", addr, 300*time.Millisecond); err == nil {
			_ = c.Close()
			return ErrOllamaPortInUse
		}
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if errors.Is(err, syscall.EADDRINUSE) || strings.Contains(strings.ToLower(err.Error()), "in use") ||
			strings.Contains(strings.ToLower(err.Error()), "only one usage") {
			return ErrOllamaPortInUse
		}
		return err
	}
	srv := &http.Server{
		Handler:           s.ollamaPortHandler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	s.ollamaPort.mu.Lock()
	s.ollamaPort.server, s.ollamaPort.addr = srv, ln.Addr().String()
	s.ollamaPort.mu.Unlock()
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) && s.deps.Logger != nil {
			s.deps.Logger.Warn("ollama port stopped", "error", err)
		}
	}()
	return nil
}

// StopOllamaPort closes the listener and its connections.
func (s *Server) StopOllamaPort() {
	s.ollamaPort.mu.Lock()
	srv := s.ollamaPort.server
	s.ollamaPort.server, s.ollamaPort.addr = nil, ""
	s.ollamaPort.mu.Unlock()
	if srv != nil {
		_ = srv.Close()
	}
}

// OllamaPortAddr is the address the listener is on, or "" when it's off.
func (s *Server) OllamaPortAddr() string {
	s.ollamaPort.mu.Lock()
	defer s.ollamaPort.mu.Unlock()
	return s.ollamaPort.addr
}
