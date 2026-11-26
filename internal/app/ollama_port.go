package app

import (
	"errors"
	"net"
	"strconv"

	"github.com/yeixio/toskar-core/internal/api"
)

// applyOllamaPort starts or stops the listener on Ollama's port to match
// its setting (#1396). It listens on this computer only: an app on another
// device can be given Toskar's own address instead. It never takes the port
// from another program, such as Ollama itself.
func (a *App) applyOllamaPort() error {
	if a.API == nil || a.Config == nil {
		return nil
	}
	if !a.Config.Get().OllamaPort {
		a.API.StopOllamaPort()
		a.ollamaPortErr.Store("")
		return nil
	}
	err := a.API.ServeOllamaPort(net.JoinHostPort("127.0.0.1", strconv.Itoa(api.OllamaPort)))
	switch {
	case err == nil:
		a.ollamaPortErr.Store("")
	case errors.Is(err, api.ErrOllamaPortInUse):
		a.ollamaPortErr.Store("in_use")
	default:
		a.ollamaPortErr.Store(err.Error())
	}
	return err
}

// ollamaPortError is why the Ollama port isn't listening while it's on.
func (a *App) ollamaPortError() string {
	if s, ok := a.ollamaPortErr.Load().(string); ok && a.Config != nil && a.Config.Get().OllamaPort {
		return s
	}
	return ""
}
