// Package portmap opens a port on the home router for access from anywhere
// (#456, docs/remote-access.md): PCP, then NAT-PMP, then UPnP-IGD, with no
// dependencies. A mapping is renewed while it's wanted and removed when it
// isn't, so a port is never left open for good.
package portmap

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"
)

// Methods a port can be mapped with.
const (
	MethodPCP    = "pcp"
	MethodNATPMP = "nat-pmp"
	MethodUPnP   = "upnp"
)

// Lifetime is what a mapping is asked for; it's renewed at half of what the
// router grants.
const Lifetime = 2 * time.Hour

// Mapping is a port the router forwards to this computer.
type Mapping struct {
	Method string
	// External is the router's outside address and port.
	External     netip.AddrPort
	InternalPort uint16
	Lifetime     time.Duration

	gateway netip.AddrPort
	nonce   [12]byte
	upnp    *igd
}

// Errors a router's answer is sorted by.
var (
	// ErrRefused is a router that answered and wouldn't open the port.
	ErrRefused = errors.New("the router refused")
	// ErrPortTaken is an outside port already forwarded to another device.
	ErrPortTaken = errors.New("the port is already forwarded to another device")
	// ErrNoPublicAddress is a router whose own outside address isn't on the
	// internet: a provider's shared address (carrier-grade NAT) or another
	// router in front of it. Routers say so instead of mapping (#617):
	// NAT-PMP and PCP with "network failure", UPnP by giving no outside
	// address, and they hide the address, so which of the two it is can't
	// be told.
	ErrNoPublicAddress = errors.New("the router has no public address of its own")
)

// Why a mapping didn't work, for Settings (#602).
const (
	WhyNoGateway = "no_gateway"
	// WhyNoAnswer is no router answering PCP, NAT-PMP, or UPnP: it offers
	// none of them, or they're turned off in its settings.
	WhyNoAnswer  = "no_answer"
	WhyRefused   = "refused"
	WhyPortTaken = "port_taken"
	// WhyNoPublicAddress is ErrNoPublicAddress: the relay is the way in.
	WhyNoPublicAddress = "no_public_address"
)

// Why sorts the error Map ended with: the router has no public address,
// the port is taken, the router refused, there's no router, or no router
// answered. "" for no error.
func Why(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrNoPublicAddress):
		return WhyNoPublicAddress
	case errors.Is(err, ErrPortTaken):
		return WhyPortTaken
	case errors.Is(err, ErrRefused):
		return WhyRefused
	case errors.Is(err, ErrNoGateway):
		return WhyNoGateway
	}
	return WhyNoAnswer
}

// DoubleNAT reports a router whose own outside address is on a private
// network: it sits behind another router, so a port it opens doesn't reach
// the internet.
func DoubleNAT(external netip.Addr) bool {
	external = external.Unmap()
	return external.IsValid() && external.IsPrivate()
}

// Router is how a mapping is made. The defaults find the router; tests
// point them at a fake one.
type Router struct {
	// Gateway is the router PCP and NAT-PMP are sent to; empty finds the
	// default gateway.
	Gateway netip.AddrPort
	// SkipUPnP leaves UPnP out.
	SkipUPnP bool
}

// Map asks the router to forward an outside port (external, or the same as
// internal when 0) to this computer's internal TCP port, trying PCP, then
// NAT-PMP, then UPnP-IGD. The error says why none worked.
func (r Router) Map(ctx context.Context, internal, external uint16) (Mapping, error) {
	if external == 0 {
		external = internal
	}
	var errs []error
	gw := r.Gateway
	if !gw.IsValid() {
		if ip, err := Gateway(ctx); err == nil {
			gw = netip.AddrPortFrom(ip, pmpPort)
		} else {
			errs = append(errs, err)
		}
	}
	if gw.IsValid() {
		m, err := pcpMap(ctx, gw, internal, external, Lifetime, newNonce())
		if err == nil {
			return m, nil
		}
		errs = append(errs, fmt.Errorf("pcp: %w", err))
		m, err = natpmpMap(ctx, gw, internal, external, Lifetime)
		if err == nil {
			return m, nil
		}
		errs = append(errs, fmt.Errorf("nat-pmp: %w", err))
	}
	if !r.SkipUPnP {
		g, err := discoverIGD(ctx)
		if err == nil {
			m, err := upnpMap(ctx, g, internal, external, Lifetime)
			if err == nil {
				return m, nil
			}
			errs = append(errs, err)
		} else {
			errs = append(errs, err)
		}
	}
	return Mapping{}, fmt.Errorf("the router didn't open a port: %w", errors.Join(errs...))
}

// renew asks the same router, the same way, to keep the mapping.
func (m Mapping) renew(ctx context.Context) (Mapping, error) {
	switch m.Method {
	case MethodPCP:
		return pcpMap(ctx, m.gateway, m.InternalPort, m.External.Port(), Lifetime, m.nonce)
	case MethodNATPMP:
		return natpmpMap(ctx, m.gateway, m.InternalPort, m.External.Port(), Lifetime)
	case MethodUPnP:
		return upnpMap(ctx, m.upnp, m.InternalPort, m.External.Port(), Lifetime)
	}
	return Mapping{}, errors.New("unknown mapping")
}

// Unmap asks the router to remove the mapping.
func (m Mapping) Unmap(ctx context.Context) error {
	switch m.Method {
	case MethodPCP:
		_, err := pcpMap(ctx, m.gateway, m.InternalPort, m.External.Port(), 0, m.nonce)
		return err
	case MethodNATPMP:
		_, err := natpmpMap(ctx, m.gateway, m.InternalPort, 0, 0)
		return err
	case MethodUPnP:
		return upnpUnmap(ctx, m.upnp, m.External.Port())
	}
	return nil
}

// Public reports an outside address the internet can reach: not private,
// not shared by a carrier (100.64/10), not loopback or link-local.
func Public(a netip.Addr) bool {
	a = a.Unmap()
	return a.IsValid() && a.IsGlobalUnicast() && !a.IsPrivate() && !carrierNAT.Contains(a)
}

var carrierNAT = netip.MustParsePrefix("100.64.0.0/10")

// CarrierNAT reports a router whose own outside address isn't public: the
// provider shares one address among many homes, so a mapping on this
// router doesn't reach the internet.
func CarrierNAT(external netip.Addr) bool {
	external = external.Unmap()
	return external.IsValid() && !Public(external)
}

// PublicIPv6 are this computer's global IPv6 addresses, which the internet
// reaches with no mapping when the router lets it.
func PublicIPv6() []netip.Addr {
	var out []netip.Addr
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			pfx, err := netip.ParsePrefix(a.String())
			if err != nil {
				continue
			}
			ip := pfx.Addr()
			// Not unique-local (fc00::/7), which the internet can't reach.
			if ip.Is6() && !ip.Is4In6() && ip.IsGlobalUnicast() && !ip.IsPrivate() {
				out = append(out, ip)
			}
		}
	}
	return out
}

// Keeper holds a mapping for a port while it's wanted: it maps, renews at
// half the granted lifetime, tries again after a failure, and removes the
// mapping when stopped.
type Keeper struct {
	Router Router

	mu      sync.Mutex
	current Mapping
	err     error
	cancel  context.CancelFunc
	done    chan struct{}
}

// Start keeps a mapping for internal, replacing what it kept before.
func (k *Keeper) Start(internal uint16) {
	k.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	k.mu.Lock()
	k.cancel, k.done = cancel, done
	k.mu.Unlock()
	go k.run(ctx, internal, done)
}

func (k *Keeper) run(ctx context.Context, internal uint16, done chan struct{}) {
	defer close(done)
	var m Mapping
	for {
		attempt, cancel := context.WithTimeout(ctx, 20*time.Second)
		var err error
		if m.Method == "" {
			m, err = k.Router.Map(attempt, internal, 0)
		} else if m, err = m.renew(attempt); err != nil {
			// The router forgot it, or another took its place: map anew.
			m, err = k.Router.Map(attempt, internal, 0)
		}
		cancel()
		k.mu.Lock()
		k.current, k.err = m, err
		k.mu.Unlock()
		// Renew at half the granted lifetime, at most once a minute; after a
		// failure, try again in five.
		wait := 5 * time.Minute
		if err == nil {
			wait = max(m.Lifetime/2, time.Minute)
		} else {
			m = Mapping{}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// Stop stops renewing and removes the mapping from the router.
func (k *Keeper) Stop() {
	k.mu.Lock()
	cancel, done, m := k.cancel, k.done, k.current
	k.cancel, k.done, k.current, k.err = nil, nil, Mapping{}, nil
	k.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
	if m.Method != "" {
		ctx, cancelUnmap := context.WithTimeout(context.Background(), 5*time.Second)
		_ = m.Unmap(ctx)
		cancelUnmap()
	}
}

// Status is the mapping kept, or why there's none.
func (k *Keeper) Status() (Mapping, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.current, k.err
}
