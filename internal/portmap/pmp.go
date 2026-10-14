package portmap

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"
)

// PCP (RFC 6887) and NAT-PMP (RFC 6886) both ask the router on UDP 5351.
// PCP comes first; a router that speaks only NAT-PMP answers PCP with
// "unsupported version", and gets NAT-PMP.

// pmpPort is where routers listen for PCP and NAT-PMP.
const pmpPort = 5351

// errUnsupportedVersion is a router that doesn't speak the version asked.
var errUnsupportedVersion = errors.New("unsupported version")

// pcpNetworkFailure and pmpNetworkFailure are the routers' answer when they
// have no outside address of their own to map on (ErrNoPublicAddress).
const (
	pcpNetworkFailure = 7
	pmpNetworkFailure = 3
)

// pcpCannotProvideExternal is PCP's answer when the outside port asked for
// is taken (RFC 6887 §7.4).
const pcpCannotProvideExternal = 11

// exchange sends req to the router and returns the first answer, trying
// again after 250 ms, 500 ms, and 1 s, as both RFCs suggest.
func exchange(ctx context.Context, gw netip.AddrPort, req []byte) ([]byte, netip.Addr, error) {
	conn, err := net.DialUDP("udp4", nil, net.UDPAddrFromAddrPort(gw))
	if err != nil {
		return nil, netip.Addr{}, err
	}
	defer conn.Close()
	local := conn.LocalAddr().(*net.UDPAddr).AddrPort().Addr().Unmap()
	buf := make([]byte, 1100)
	wait := 250 * time.Millisecond
	for try := 0; try < 4; try++ {
		if err := ctx.Err(); err != nil {
			return nil, local, err
		}
		if _, err := conn.Write(req); err != nil {
			return nil, local, err
		}
		deadline := time.Now().Add(wait)
		if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
			deadline = d
		}
		_ = conn.SetReadDeadline(deadline)
		n, err := conn.Read(buf)
		if err == nil {
			return buf[:n], local, nil
		}
		var ne net.Error
		if !errors.As(err, &ne) || !ne.Timeout() {
			// A refused port: nothing listens for this protocol.
			return nil, local, err
		}
		wait *= 2
	}
	return nil, local, context.DeadlineExceeded
}

// pcpMap asks for a TCP mapping of internal to external (0 for any) with
// PCP, for lifetime; lifetime 0 removes the mapping that nonce made.
func pcpMap(ctx context.Context, gw netip.AddrPort, internal, external uint16, lifetime time.Duration, nonce [12]byte) (Mapping, error) {
	req := make([]byte, 60)
	req[0], req[1] = 2, 1 // version 2, MAP request
	binary.BigEndian.PutUint32(req[4:8], uint32(lifetime/time.Second))
	// The client's address is filled in once the socket knows it.
	copy(req[24:36], nonce[:])
	req[36] = 6 // TCP
	binary.BigEndian.PutUint16(req[40:42], internal)
	binary.BigEndian.PutUint16(req[42:44], external)
	copy(req[44:60], mapped4(netip.IPv4Unspecified()))
	// The client address must be the one the router sees, so learn it from
	// a socket toward the router first.
	local, err := localAddrToward(gw)
	if err != nil {
		return Mapping{}, err
	}
	copy(req[8:24], mapped4(local))
	resp, _, err := exchange(ctx, gw, req)
	if err != nil {
		return Mapping{}, err
	}
	if len(resp) >= 4 && resp[0] == 0 {
		// A NAT-PMP router answering a version it doesn't know.
		return Mapping{}, errUnsupportedVersion
	}
	if len(resp) < 60 || resp[0] != 2 || resp[1] != 0x81 {
		return Mapping{}, fmt.Errorf("pcp: unexpected answer")
	}
	if code := resp[3]; code != 0 {
		if code == 1 {
			return Mapping{}, errUnsupportedVersion
		}
		switch code {
		case pcpCannotProvideExternal:
			return Mapping{}, fmt.Errorf("pcp: %w (result %d)", ErrPortTaken, code)
		case pcpNetworkFailure:
			return Mapping{}, fmt.Errorf("pcp: %w (result %d)", ErrNoPublicAddress, code)
		}
		return Mapping{}, fmt.Errorf("pcp: %w (result %d)", ErrRefused, code)
	}
	if [12]byte(resp[24:36]) != nonce {
		return Mapping{}, fmt.Errorf("pcp: an answer to another request")
	}
	ext := netip.AddrFrom16([16]byte(resp[44:60])).Unmap()
	return Mapping{
		Method:       MethodPCP,
		External:     netip.AddrPortFrom(ext, binary.BigEndian.Uint16(resp[42:44])),
		InternalPort: internal,
		Lifetime:     time.Duration(binary.BigEndian.Uint32(resp[4:8])) * time.Second,
		gateway:      gw,
		nonce:        nonce,
	}, nil
}

// natpmpMap asks for a TCP mapping with NAT-PMP; lifetime 0 removes it.
func natpmpMap(ctx context.Context, gw netip.AddrPort, internal, external uint16, lifetime time.Duration) (Mapping, error) {
	req := make([]byte, 12)
	req[0], req[1] = 0, 2 // version 0, map TCP
	binary.BigEndian.PutUint16(req[4:6], internal)
	binary.BigEndian.PutUint16(req[6:8], external)
	binary.BigEndian.PutUint32(req[8:12], uint32(lifetime/time.Second))
	resp, _, err := exchange(ctx, gw, req)
	if err != nil {
		return Mapping{}, err
	}
	if len(resp) < 16 || resp[0] != 0 || resp[1] != 130 {
		return Mapping{}, fmt.Errorf("nat-pmp: unexpected answer")
	}
	if code := binary.BigEndian.Uint16(resp[2:4]); code == pmpNetworkFailure {
		return Mapping{}, fmt.Errorf("nat-pmp: %w (result %d)", ErrNoPublicAddress, code)
	} else if code != 0 {
		return Mapping{}, fmt.Errorf("nat-pmp: %w (result %d)", ErrRefused, code)
	}
	m := Mapping{
		Method:       MethodNATPMP,
		InternalPort: internal,
		Lifetime:     time.Duration(binary.BigEndian.Uint32(resp[12:16])) * time.Second,
		gateway:      gw,
	}
	port := binary.BigEndian.Uint16(resp[10:12])
	if lifetime > 0 {
		ip, err := natpmpExternal(ctx, gw)
		if err != nil {
			return Mapping{}, err
		}
		m.External = netip.AddrPortFrom(ip, port)
	}
	return m, nil
}

// natpmpExternal asks the router for its outside address.
func natpmpExternal(ctx context.Context, gw netip.AddrPort) (netip.Addr, error) {
	resp, _, err := exchange(ctx, gw, []byte{0, 0})
	if err != nil {
		return netip.Addr{}, err
	}
	if len(resp) < 12 || resp[0] != 0 || resp[1] != 128 {
		return netip.Addr{}, fmt.Errorf("nat-pmp: unexpected answer")
	}
	if code := binary.BigEndian.Uint16(resp[2:4]); code == pmpNetworkFailure {
		return netip.Addr{}, fmt.Errorf("nat-pmp: %w (result %d)", ErrNoPublicAddress, code)
	} else if code != 0 {
		return netip.Addr{}, fmt.Errorf("nat-pmp: the router refused (result %d)", code)
	}
	return netip.AddrFrom4([4]byte(resp[8:12])), nil
}

func localAddrToward(gw netip.AddrPort) (netip.Addr, error) {
	conn, err := net.DialUDP("udp4", nil, net.UDPAddrFromAddrPort(gw))
	if err != nil {
		return netip.Addr{}, err
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).AddrPort().Addr().Unmap(), nil
}

func mapped4(a netip.Addr) []byte {
	b := netip.AddrFrom16(a.As16()).As16()
	if a.Is4() {
		b = netip.AddrFrom16([16]byte{10: 0xff, 11: 0xff, 12: a.As4()[0], 13: a.As4()[1], 14: a.As4()[2], 15: a.As4()[3]}).As16()
	}
	return b[:]
}

func newNonce() [12]byte {
	var n [12]byte
	_, _ = rand.Read(n[:])
	return n
}
