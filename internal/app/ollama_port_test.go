package app

import (
	"errors"
	"net"
	"testing"

	"github.com/yeixio/toskar-core/internal/api"
	"github.com/yeixio/toskar-core/internal/config"
)

// With Ollama's port held by another program, turning it on leaves it to
// that program and says why (#1396).
func TestOllamaPortInUse(t *testing.T) {
	mgr, err := config.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := &App{Config: mgr, API: api.NewServer(api.Dependencies{Config: mgr})}
	// Hold the port, unless something on this computer already does.
	if held, err := net.Listen("tcp", "127.0.0.1:11434"); err == nil {
		defer held.Close()
	}
	if err := a.applyOllamaPort(); err != nil || a.ollamaPortError() != "" {
		t.Fatalf("off: %v %q", err, a.ollamaPortError())
	}
	if err := mgr.Update(func(c *config.Config) { c.OllamaPort = true }); err != nil {
		t.Fatal(err)
	}
	if err := a.applyOllamaPort(); !errors.Is(err, api.ErrOllamaPortInUse) {
		t.Fatalf("held port: %v", err)
	}
	if a.ollamaPortError() != "in_use" || a.API.OllamaPortAddr() != "" {
		t.Fatalf("error %q, listening on %q", a.ollamaPortError(), a.API.OllamaPortAddr())
	}
	if err := mgr.Update(func(c *config.Config) { c.OllamaPort = false }); err != nil {
		t.Fatal(err)
	}
	if err := a.applyOllamaPort(); err != nil || a.ollamaPortError() != "" {
		t.Fatalf("off again: %v %q", err, a.ollamaPortError())
	}
}
