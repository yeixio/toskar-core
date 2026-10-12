package app

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/yeixio/toskar-core/internal/api"
	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/portmap"
	"github.com/yeixio/toskar-core/internal/rendezvous"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Access from anywhere (#456, docs/remote-access.md): the listener for paired
// devices outside the home network, started and stopped with its setting.

// applyRemoteAccess starts the remote listener when access from anywhere is
// on, on every address, or stops it and drops its connections when it's off.
func (a *App) applyRemoteAccess() error {
	if a.API == nil || a.Config == nil {
		return nil
	}
	cfg := a.Config.Get()
	if !cfg.RemoteAccess.Enabled {
		a.stopRelay()
		a.portKeeper.Stop()
		a.API.StopRemote()
		return nil
	}
	if err := a.API.ServeRemote(fmt.Sprintf(":%d", cfg.RemotePort())); err != nil {
		a.stopRelay()
		a.portKeeper.Stop()
		return err
	}
	// The router opens a port for it, unless the person forwarded one or
	// turned that off (docs/remote-access.md).
	if mapsPort(cfg) {
		a.portKeeper.Start(uint16(cfg.RemotePort()))
	} else {
		a.portKeeper.Stop()
	}
	// The relay tunnel hands devices to the listener just started.
	a.applyRelay()
	return nil
}

// mapsPort reports whether Toskar asks the router for a port.
func mapsPort(cfg config.Config) bool {
	return cfg.RemoteAccess.Enabled && !cfg.RemoteAccess.NoPortMapping && cfg.RemoteAccess.Address == ""
}

// remoteReach is how the internet reaches the remote listener, for its
// status in Settings.
func (a *App) remoteReach() api.RemoteReach {
	cfg := a.Config.Get()
	out := api.RemoteReach{PortMapping: mapsPort(cfg)}
	for _, ip := range portmap.PublicIPv6() {
		out.IPv6 = append(out.IPv6, ip.String())
	}
	m, err := a.portKeeper.Status()
	if m.Method != "" {
		out.Mapped, out.Method = m.External.String(), m.Method
	}
	if err != nil {
		out.MapError = err.Error()
	}
	listening := a.API != nil && a.API.RemoteListening().Listening != ""
	relayed := false
	if name, st := a.relayStatus(); st != nil {
		out.Relay, out.RelayState, out.RelayError = name, st.State, st.Error
		relayed = st.State == "connected"
	}
	out.Reachable, out.Reason = reachOf(cfg, listening, m, len(out.IPv6) > 0, relayed)
	out.Check = a.reachCheck()
	return out
}

// reachOf says in a word how the internet reaches the listener, and why
// not when it doesn't. The relay counts only when nothing direct does.
func reachOf(cfg config.Config, listening bool, m portmap.Mapping, ipv6, relayed bool) (reachable, reason string) {
	switch {
	case !cfg.RemoteAccess.Enabled:
		return "none", "off"
	case !listening:
		return "none", "not_listening"
	case cfg.RemoteAccess.Address != "":
		return "manual", ""
	case m.Method != "" && portmap.Public(m.External.Addr()):
		return "direct", ""
	case ipv6:
		return "ipv6", ""
	case relayed:
		return "relay", ""
	case m.Method != "" && portmap.CarrierNAT(m.External.Addr()):
		return "none", "carrier_nat"
	}
	return "none", "no_port"
}

// Errors from the remote access settings.
var (
	errRemotePort    = contracts.NewError("REMOTE_PORT_INVALID", nil, errors.New("the port for access from anywhere is 1024 to 65535, and not the API's or the computers' port"))
	errRemoteAddress = contracts.NewError("REMOTE_ADDRESS_INVALID", nil, errors.New("the address is a host name or IP, with a port when it differs, such as home.example.com or 203.0.113.7:7333"))
)

// validRemotePort reports a port the remote listener can take.
func validRemotePort(cfg config.Config, port int) bool {
	return port >= 1024 && port <= 65535 && port != cfg.APIPort && port != cfg.InternalPort
}

// cleanRemoteAddress checks a forwarded address, such as
// "home.example.com" or "203.0.113.7:7333"; "" clears it.
func cleanRemoteAddress(in string) (string, error) {
	addr := strings.TrimSpace(in)
	if addr == "" {
		return "", nil
	}
	if strings.Contains(addr, "://") || strings.ContainsAny(addr, " /?#@") || len(addr) > 260 {
		return "", errRemoteAddress
	}
	host := addr
	if h, port, err := net.SplitHostPort(addr); err == nil {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", errRemoteAddress
		}
		host = h
	} else if strings.Count(addr, ":") == 1 {
		return "", errRemoteAddress
	}
	host = strings.Trim(host, "[]")
	if host == "" || (net.ParseIP(host) == nil && !validHostName(host)) {
		return "", errRemoteAddress
	}
	return addr, nil
}

func validHostName(h string) bool {
	if len(h) > 253 {
		return false
	}
	for _, label := range strings.Split(strings.TrimSuffix(h, "."), ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, r := range label {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			default:
				return false
			}
		}
	}
	return true
}

// routeSecret is this computer's route secret for paired devices, made the
// first time, and its route ID.
func (a *App) routeSecret() (string, string, error) {
	secret, err := rendezvous.RouteSecret(auth.NewSecretStore(a.Config.Get().DataDir))
	if err != nil {
		return "", "", err
	}
	return rendezvous.EncodeSecret(secret), rendezvous.RouteID(secret), nil
}
