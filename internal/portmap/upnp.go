package portmap

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// UPnP Internet Gateway Device: find the router by SSDP multicast, read its
// description, and ask its WAN connection service for a port mapping over
// SOAP.

// ssdpAddr is SSDP's multicast address; tests point it elsewhere.
var ssdpAddr = "239.255.255.250:1900"

// wanServices are the services that map ports, in the order they're tried.
var wanServices = []string{
	"urn:schemas-upnp-org:service:WANIPConnection:2",
	"urn:schemas-upnp-org:service:WANIPConnection:1",
	"urn:schemas-upnp-org:service:WANPPPConnection:1",
}

// igd is a router's port mapping service.
type igd struct {
	control string // the service's control URL
	service string // its type
	local   netip.Addr
}

// upnpConflict is UPnP's ConflictInMappingEntry: the outside port is
// already forwarded to another device.
const upnpConflict = "718"

// discoverIGD finds a router's WAN connection service.
func discoverIGD(ctx context.Context) (*igd, error) {
	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	dst, err := net.ResolveUDPAddr("udp4", ssdpAddr)
	if err != nil {
		return nil, err
	}
	search := "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nMX: 2\r\n" +
		"ST: urn:schemas-upnp-org:device:InternetGatewayDevice:1\r\n\r\n"
	deadline := time.Now().Add(3 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetReadDeadline(deadline)
	if _, err := conn.WriteToUDP([]byte(search), dst); err != nil {
		return nil, err
	}
	buf := make([]byte, 2048)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			return nil, errors.New("upnp: no router answered")
		}
		loc := headerValue(string(buf[:n]), "location")
		if loc == "" || !sameHost(loc, from.AddrPort().Addr().Unmap()) {
			// Only the device that answered, on this network, is asked.
			continue
		}
		dev, err := readDescription(ctx, loc)
		if err != nil {
			continue
		}
		// The address the router sees for this computer.
		local, err := localAddrToward(netip.AddrPortFrom(from.AddrPort().Addr().Unmap(), 1900))
		if err != nil {
			continue
		}
		dev.local = local
		return dev, nil
	}
}

// sameHost reports a description URL on the device that answered, at a
// private address.
func sameHost(location string, from netip.Addr) bool {
	u, err := url.Parse(location)
	if err != nil {
		return false
	}
	host, err := netip.ParseAddr(u.Hostname())
	return err == nil && host.Unmap() == from && (from.IsPrivate() || from.IsLoopback() || from.IsLinkLocalUnicast())
}

func headerValue(resp, name string) string {
	for _, line := range strings.Split(resp, "\r\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), name) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

type upnpDevice struct {
	Services []struct {
		Type    string `xml:"serviceType"`
		Control string `xml:"controlURL"`
	} `xml:"serviceList>service"`
	Devices []upnpDevice `xml:"deviceList>device"`
}

// readDescription reads a router's description and finds a service that
// maps ports.
func readDescription(ctx context.Context, location string) (*igd, error) {
	base, err := url.Parse(location)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") {
		return nil, errors.New("upnp: bad location")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	res, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var root struct {
		URLBase string     `xml:"URLBase"`
		Device  upnpDevice `xml:"device"`
	}
	if err := xml.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&root); err != nil {
		return nil, err
	}
	if root.URLBase != "" {
		if u, err := url.Parse(root.URLBase); err == nil {
			base = u
		}
	}
	for _, want := range wanServices {
		if control := findService(root.Device, want); control != "" {
			ref, err := url.Parse(control)
			if err != nil {
				continue
			}
			return &igd{control: base.ResolveReference(ref).String(), service: want}, nil
		}
	}
	return nil, errors.New("upnp: the router has no port mapping service")
}

func findService(d upnpDevice, want string) string {
	for _, s := range d.Services {
		if strings.TrimSpace(s.Type) == want {
			return strings.TrimSpace(s.Control)
		}
	}
	for _, sub := range d.Devices {
		if c := findService(sub, want); c != "" {
			return c
		}
	}
	return ""
}

// soap calls an action on the service and returns the response body.
func (g *igd) soap(ctx context.Context, action string, args [][2]string) ([]byte, error) {
	var body bytes.Buffer
	body.WriteString(`<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>`)
	fmt.Fprintf(&body, `<u:%s xmlns:u="%s">`, action, g.service)
	for _, a := range args {
		body.WriteString("<" + a[0] + ">")
		_ = xml.EscapeText(&body, []byte(a[1]))
		body.WriteString("</" + a[0] + ">")
	}
	fmt.Fprintf(&body, `</u:%s></s:Body></s:Envelope>`, action)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, g.control, &body)
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPAction", `"`+g.service+"#"+action+`"`)
	res, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	if res.StatusCode != http.StatusOK {
		code := between(string(out), "<errorCode>", "</errorCode>")
		desc := between(string(out), "<errorDescription>", "</errorDescription>")
		why := ErrRefused
		if code == upnpConflict {
			why = ErrPortTaken
		}
		return nil, fmt.Errorf("upnp: %s %w (%s %s)", action, why, code, desc)
	}
	return out, nil
}

func between(s, open, close string) string {
	_, rest, ok := strings.Cut(s, open)
	if !ok {
		return ""
	}
	v, _, _ := strings.Cut(rest, close)
	return strings.TrimSpace(v)
}

// upnpMap asks the router to forward external to this computer's internal
// port for lifetime.
func upnpMap(ctx context.Context, g *igd, internal, external uint16, lifetime time.Duration) (Mapping, error) {
	if external == 0 {
		external = internal
	}
	_, err := g.soap(ctx, "AddPortMapping", [][2]string{
		{"NewRemoteHost", ""},
		{"NewExternalPort", strconv.Itoa(int(external))},
		{"NewProtocol", "TCP"},
		{"NewInternalPort", strconv.Itoa(int(internal))},
		{"NewInternalClient", g.local.String()},
		{"NewEnabled", "1"},
		{"NewPortMappingDescription", "Toskar"},
		{"NewLeaseDuration", strconv.Itoa(int(lifetime / time.Second))},
	})
	if err != nil {
		// A refusal from a router with no public address: it says so by
		// giving none when asked.
		if errors.Is(err, ErrRefused) {
			if ip, ok := upnpExternal(ctx, g); ok && !Public(ip) {
				return Mapping{}, fmt.Errorf("upnp: %w (it gave %q)", ErrNoPublicAddress, ip)
			}
		}
		return Mapping{}, err
	}
	ip, ok := upnpExternal(ctx, g)
	if !ok {
		return Mapping{}, errors.New("upnp: the router didn't say its outside address")
	}
	if !ip.IsValid() || ip.IsUnspecified() {
		return Mapping{}, fmt.Errorf("upnp: %w", ErrNoPublicAddress)
	}
	return Mapping{
		Method:       MethodUPnP,
		External:     netip.AddrPortFrom(ip.Unmap(), external),
		InternalPort: internal,
		Lifetime:     lifetime,
		upnp:         g,
	}, nil
}

// upnpExternal is the router's outside address, as it says it; ok is false
// when it didn't answer. A router with no outside address gives an empty or
// 0.0.0.0 one, which is the zero or unspecified Addr here.
func upnpExternal(ctx context.Context, g *igd) (netip.Addr, bool) {
	out, err := g.soap(ctx, "GetExternalIPAddress", nil)
	if err != nil {
		return netip.Addr{}, false
	}
	text := between(string(out), "<NewExternalIPAddress>", "</NewExternalIPAddress>")
	if text == "" {
		return netip.Addr{}, true
	}
	ip, err := netip.ParseAddr(text)
	if err != nil {
		return netip.Addr{}, false
	}
	return ip.Unmap(), true
}

// upnpUnmap removes a mapping.
func upnpUnmap(ctx context.Context, g *igd, external uint16) error {
	_, err := g.soap(ctx, "DeletePortMapping", [][2]string{
		{"NewRemoteHost", ""},
		{"NewExternalPort", strconv.Itoa(int(external))},
		{"NewProtocol", "TCP"},
	})
	return err
}
