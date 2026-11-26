package api

import (
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/config"
)

// The Ollama port (#1396) answers Ollama's routes through the API's router,
// and the root as Ollama does, but nothing else of Toskar's; and it never
// takes a port another program has.
func TestOllamaPort(t *testing.T) {
	mgr, err := config.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Dependencies{Config: mgr})
	if err := srv.ServeOllamaPort("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.StopOllamaPort)
	base := "http://" + srv.OllamaPortAddr()
	get := func(path string) (int, string) {
		t.Helper()
		resp, err := http.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}
	if code, body := get("/"); code != http.StatusOK || body != "Ollama is running" {
		t.Fatalf("root: %d %q", code, body)
	}
	// Ollama's routes reach the API's handlers, from this computer with no
	// key, as on the API's own address: here, the router's answer without a
	// model handler.
	if code, body := get("/api/tags"); code != http.StatusNotImplemented || !strings.Contains(body, "NOT_IMPLEMENTED") {
		t.Fatalf("tags: %d %s", code, body)
	}
	// Toskar's own API and the OpenAI routes aren't on this port.
	for _, path := range []string{"/api/v1/health", "/v1/models", "/api/v1/settings", "/index.html"} {
		if code, body := get(path); code != http.StatusNotFound || body != `{"error":"404 page not found"}` {
			t.Fatalf("%s: %d %s", path, code, body)
		}
	}

	// Another program on the port: refused, and nothing listens.
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if err := srv.ServeOllamaPort(held.Addr().String()); !errors.Is(err, ErrOllamaPortInUse) {
		t.Fatalf("held port: %v", err)
	}
	if srv.OllamaPortAddr() != "" {
		t.Fatalf("listening on %s after a refused port", srv.OllamaPortAddr())
	}
}
