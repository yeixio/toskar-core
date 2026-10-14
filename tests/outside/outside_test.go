//go:build outside

// The outside-network check (#617): a real Core behind a real router, a
// relay on a simulated internet, and a paired device at home and away, in
// Docker. Each scenario asserts what the device reached and what Core's
// own outside check (#546) said, so a computer that says it's reachable is.
//
//	home      10.77.0.0/24    Core .10, the router .2, a device at home .20
//	internet  11.0.0.0/24     the router .2, the relay .10, a device away .20
//	carrier   100.64.0.0/24   a carrier's NAT between the router and the internet
//
// The internet is 11.0.0.0/24 because Core and the relay treat private and
// documentation ranges as not public. Docker doesn't NAT these networks, so
// nothing on them is answered from the real internet, and the device and
// the relay have no way out but these networks. They aren't Docker
// "internal" networks: those drop routed packets and multicast, which a
// router and UPnP need.
//
// Run it with Docker and a toskar-relay checkout:
//
//	TOSKAR_RELAY_SRC=../toskar-relay go test -tags outside -v -timeout 30m ./tests/outside
package outside_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	image     = "toskar-outside:test"
	prefix    = "toskar-outside-"
	homeNet   = prefix + "home"
	inetNet   = prefix + "internet"
	carrNet   = prefix + "carrier"
	coreIP    = "10.77.0.10"
	routerLAN = "10.77.0.2"
	routerWAN = "11.0.0.2"
	carrierIP = "100.64.0.10" // the router's outside address behind carrier NAT
	ispLAN    = "100.64.0.2"
	ispWAN    = "11.0.0.3"
	relayIP   = "11.0.0.10"
	homeDev   = "10.77.0.20"
	awayDev   = "11.0.0.20"
	relayName = "relay.toskar.test"
	apiKey    = "outside-check-bootstrap-key-0123456789"
	// relayRecover is how long a device may be cut off when the relay
	// restarts. Relays before toskar-relay#34 said REPLACED as they
	// stopped, and Core then stayed away a full minute.
	relayRecover = 20 * time.Second
)

var (
	binDir   string // toskar, client, toskar-relay for Linux
	relayDir string // the relay's certificate, keys, and enrollment secret
)

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	src := os.Getenv("TOSKAR_RELAY_SRC")
	if src == "" {
		fmt.Println("TOSKAR_RELAY_SRC isn't set: the outside-network check needs a toskar-relay checkout")
		return 0
	}
	if _, err := exec.LookPath("docker"); err != nil {
		fmt.Println("no docker: the outside-network check needs it")
		return 1
	}
	tmp, err := os.MkdirTemp("", "toskar-outside-")
	if err != nil {
		fmt.Println(err)
		return 1
	}
	defer os.RemoveAll(tmp)
	binDir, relayDir = filepath.Join(tmp, "bin"), filepath.Join(tmp, "relay")
	_ = os.MkdirAll(relayDir, 0o755)
	if err := setup(src); err != nil {
		fmt.Println("setup:", err)
		teardown()
		return 1
	}
	defer teardown()
	return m.Run()
}

// setup builds the programs, the image, the networks, and the relay's keys.
func setup(relaySrc string) error {
	root, _ := filepath.Abs("../..")
	build := func(dir, out, pkg string) error {
		cmd := exec.Command("go", "build", "-o", filepath.Join(binDir, out), pkg)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+runtime.GOARCH)
		if b, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("building %s: %v\n%s", out, err, b)
		}
		return nil
	}
	if err := build(root, "toskar", "./cmd/daemon"); err != nil {
		return err
	}
	if err := build(root, "client", "./tests/outside/client"); err != nil {
		return err
	}
	if err := build(relaySrc, "toskar-relay", "./cmd/toskar-relay"); err != nil {
		return err
	}
	if _, err := docker("build", "-q", "-t", image, "."); err != nil {
		return err
	}
	teardown()
	for _, n := range [][2]string{{homeNet, "10.77.0.0/24"}, {inetNet, "11.0.0.0/24"}, {carrNet, "100.64.0.0/24"}} {
		if _, err := docker("network", "create", "--subnet", n[1], "-o", "com.docker.network.bridge.enable_ip_masquerade=false", n[0]); err != nil {
			return err
		}
	}
	if err := relayCertificate(); err != nil {
		return err
	}
	// The relay's signing key and enrollment secret, made by the relay.
	if _, err := docker("run", "--rm", "-v", binDir+":/opt/bin:ro", "-v", relayDir+":/relay", image,
		"sh", "-c", "/opt/bin/toskar-relay keygen -key /relay/signing.key && /opt/bin/toskar-relay enroll-secret -out /relay/enroll.secret && chmod a+r /relay/*"); err != nil {
		return err
	}
	return nil
}

func teardown() {
	out, _ := docker("ps", "-aq", "--filter", "name="+prefix)
	if ids := strings.Fields(out); len(ids) > 0 {
		_, _ = docker(append([]string{"rm", "-f"}, ids...)...)
	}
	for _, n := range []string{homeNet, inetNet, carrNet} {
		_, _ = docker("network", "rm", n)
	}
}

// relayCertificate makes a certificate authority and the relay's
// certificate from it; Core trusts it through TOSKAR_RELAY_CA.
func relayCertificate() error {
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ca := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Toskar outside check CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		return err
	}
	ca, _ = x509.ParseCertificate(caDER)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leaf := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: relayName},
		DNSNames:  []string{relayName},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, caKey)
	if err != nil {
		return err
	}
	keyDER, _ := x509.MarshalECPrivateKey(key)
	write := func(name, kind string, b []byte) error {
		return os.WriteFile(filepath.Join(relayDir, name), pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: b}), 0o644)
	}
	if err := write("ca.pem", "CERTIFICATE", caDER); err != nil {
		return err
	}
	if err := write("tls.crt", "CERTIFICATE", der); err != nil {
		return err
	}
	return write("tls.key", "EC PRIVATE KEY", keyDER)
}

func docker(args ...string) (string, error) {
	var out, errb bytes.Buffer
	cmd := exec.Command("docker", args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("docker %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// lab is one scenario's machines.
type lab struct {
	t       *testing.T
	carrier bool
	key     string // the paired device's key
	pin     string // the certificate it paired with
	home    string // the address it paired at
	route   string // the computer's route ID
}

// router is how the home router lets a port in.
type router struct {
	mode    string // pcp, upnp, none, forward
	carrier bool   // behind a carrier's NAT
}

func newLab(t *testing.T, r router) *lab {
	t.Helper()
	t.Cleanup(func() {
		if t.Failed() {
			for _, c := range []string{"core", "router", "relay", "isp"} {
				out, _ := exec.Command("docker", "logs", "--tail", "40", prefix+c).CombinedOutput()
				t.Logf("--- %s ---\n%s", c, out)
			}
		}
		out, _ := docker("ps", "-aq", "--filter", "name="+prefix)
		if ids := strings.Fields(out); len(ids) > 0 {
			_, _ = docker(append([]string{"rm", "-f"}, ids...)...)
		}
	})
	l := &lab{t: t, carrier: r.carrier}
	l.startRelay()
	wan, wanNet, wanPrefix := routerWAN, inetNet, "11.0.0."
	env := []string{"-e", "LAN_NET=10.77.0.", "-e", "MODE=" + r.mode}
	if r.carrier {
		// The carrier's NAT: its customers on 100.64.0.0/24, the internet outside.
		l.start("isp", homeless(carrNet, ispLAN), []string{"--cap-add", "NET_ADMIN", "--sysctl", "net.ipv4.ip_forward=1",
			"-e", "LAN_NET=100.64.0.", "-e", "WAN_NET=11.0.0.", "-e", "MODE=none", "-v", mustAbs("router.sh") + ":/router.sh:ro"},
			[][2]string{{inetNet, ispWAN}}, "/router.sh")
		wan, wanNet, wanPrefix = carrierIP, carrNet, "100.64.0."
		env = append(env, "-e", "UPSTREAM="+ispLAN)
	}
	if r.mode == "forward" {
		env = append(env, "-e", "FORWARD_PORT=7333", "-e", "FORWARD_TO="+coreIP+":7333")
	}
	l.start("router", homeless(homeNet, routerLAN), append([]string{"--cap-add", "NET_ADMIN", "--sysctl", "net.ipv4.ip_forward=1",
		"-e", "WAN_NET=" + wanPrefix, "-v", mustAbs("router.sh") + ":/router.sh:ro"}, env...),
		[][2]string{{wanNet, wan}}, "/router.sh")
	l.startCore()
	l.pair()
	return l
}

func homeless(network, ip string) []string { return []string{"--network", network, "--ip", ip} }

func mustAbs(p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		panic(err)
	}
	return a
}

// start creates a container on its first network, connects the others,
// and starts it.
func (l *lab) start(name string, first, opts []string, more [][2]string, cmd ...string) {
	l.t.Helper()
	args := append([]string{"create", "--name", prefix + name}, first...)
	args = append(args, opts...)
	args = append(args, image)
	args = append(args, cmd...)
	if _, err := docker(args...); err != nil {
		l.t.Fatal(err)
	}
	for _, n := range more {
		if _, err := docker("network", "connect", "--ip", n[1], n[0], prefix+name); err != nil {
			l.t.Fatal(err)
		}
	}
	if _, err := docker("start", prefix+name); err != nil {
		l.t.Fatal(err)
	}
}

func (l *lab) startRelay() {
	l.start("relay", homeless(inetNet, relayIP), []string{"--cap-add", "NET_ADMIN", "-w", "/tmp", "-v", binDir + ":/opt/bin:ro", "-v", relayDir + ":/relay:ro"}, nil,
		"sh", "-c", `ip route del default 2>/dev/null; exec /opt/bin/toskar-relay "$@"`, "relay", "serve", "-dev", "-domain", relayName, "-listen", ":443",
		"-tls-cert", "/relay/tls.crt", "-tls-key", "/relay/tls.key", "-key", "/relay/signing.key",
		"-enroll-secret", "/relay/enroll.secret", "-open-lookups")
}

func (l *lab) startCore() {
	l.start("core", homeless(homeNet, coreIP), []string{"--cap-add", "NET_ADMIN",
		"--add-host", relayName + ":" + relayIP,
		"-v", binDir + ":/opt/bin:ro", "-v", relayDir + ":/relay:ro",
		"-e", "TOSKAR_API_HOST=0.0.0.0", "-e", "TOSKAR_API_KEY=" + apiKey,
		"-e", "TOSKAR_DISCOVERY_ENABLED=false", "-e", "TOSKAR_NODE_NAME=outside-check",
		"-e", "TOSKAR_RELAY_CA=/relay/ca.pem"}, nil,
		"sh", "-c", "ip route replace default via "+routerLAN+" && exec /opt/bin/toskar --data-dir /data")
	l.eventually(30*time.Second, "Core's API to answer", func() (bool, string) {
		_, err := l.api("GET", "/api/v1/health", "")
		return err == nil, fmt.Sprint(err)
	})
}

// api calls Core's API from Core's own computer, as its Owner.
func (l *lab) api(method, path, body string) ([]byte, error) {
	args := []string{"exec", prefix + "core", "curl", "-sS", "--fail-with-body", "-m", "60", "-X", method,
		"-H", "Content-Type: application/json", "http://127.0.0.1:7331" + path}
	if body != "" {
		args = append(args, "-d", body)
	}
	out, err := docker(args...)
	if err != nil {
		return []byte(out), fmt.Errorf("%s %s: %v %s", method, path, err, out)
	}
	return []byte(out), nil
}

func (l *lab) apiJSON(method, path, body string, v any) {
	l.t.Helper()
	b, err := l.api(method, path, body)
	if err != nil {
		l.t.Fatal(err)
	}
	if v != nil {
		if err := json.Unmarshal(b, v); err != nil {
			l.t.Fatalf("%s %s: %v: %s", method, path, err, b)
		}
	}
}

// pair pairs a device the way the app does at home: a code from the
// computer, then the device sends it.
func (l *lab) pair() {
	l.t.Helper()
	var code struct {
		Code string `json:"code"`
	}
	l.apiJSON("POST", "/api/v1/devices/pairing", `{"enable_lan":true}`, &code)
	var paired struct {
		APIKey  string `json:"api_key"`
		Pin     string `json:"tls_fingerprint"`
		RouteID string `json:"route_id"`
		Address string `json:"address"`
	}
	l.apiJSON("POST", "/api/v1/devices/pair", fmt.Sprintf(`{"code":%q,"device_name":"outside check"}`, code.Code), &paired)
	if paired.APIKey == "" || paired.Pin == "" {
		l.t.Fatalf("pairing gave no key or certificate: %+v", paired)
	}
	l.key, l.pin, l.route, l.home = paired.APIKey, paired.Pin, paired.RouteID, paired.Address
	if l.home == "" {
		l.home = coreIP + ":7331"
	}
}

// anywhere turns on access from anywhere through the lab's relay, with the
// router's port asked for or not, or a forwarded address.
func (l *lab) anywhere(portMapping bool, address string) {
	l.t.Helper()
	secret, err := os.ReadFile(filepath.Join(relayDir, "enroll.secret"))
	if err != nil {
		l.t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"remote_access_enabled":      true,
		"remote_access_port_mapping": portMapping,
		"remote_access_address":      address,
		"remote_access_relay":        relayName,
		"remote_access_relay_secret": strings.TrimSpace(string(secret)),
	})
	l.apiJSON("PATCH", "/api/v1/settings", string(body), nil)
	if l.route == "" {
		var route struct {
			RouteID string `json:"route_id"`
		}
		l.apiJSON("GET", "/api/v1/remote-access/route", "", &route)
		l.route = route.RouteID
	}
}

// reach is GET /api/v1/remote-access.
type reach struct {
	Mapped    string `json:"mapped"`
	MappedBy  string `json:"mapped_by"`
	MapError  string `json:"map_error"`
	MapReason string `json:"map_reason"`
	Reachable string `json:"reachable"`
	Reason    string `json:"reason"`
	Relay     *struct {
		State string `json:"state"`
		Error string `json:"error"`
	} `json:"relay"`
	Check *struct {
		At     time.Time `json:"at"`
		Error  string    `json:"error"`
		Relay  *way      `json:"relay"`
		Direct []way     `json:"direct"`
	} `json:"check"`
}

type way struct {
	Address string `json:"address"`
	OK      bool   `json:"ok"`
	Reason  string `json:"reason"`
}

func (l *lab) reach() reach {
	var r reach
	l.apiJSON("GET", "/api/v1/remote-access", "", &r)
	return r
}

// settled waits for the tunnel, and for the router's answer when Core
// asks it for a port.
func (l *lab) settled(portMapping bool) reach {
	l.t.Helper()
	var r reach
	l.eventually(60*time.Second, "the relay tunnel and the router's answer", func() (bool, string) {
		r = l.reach()
		up := r.Relay != nil && r.Relay.State == "connected"
		mapped := !portMapping || r.Mapped != "" || r.MapError != ""
		return up && mapped, fmt.Sprintf("%+v", r)
	})
	return r
}

// check runs Core's outside check now, waiting out the relay's once a
// minute when a check Core ran itself got there first.
func (l *lab) check() reach {
	l.t.Helper()
	var r reach
	l.eventually(150*time.Second, "Core's outside check", func() (bool, string) {
		l.apiJSON("POST", "/api/v1/remote-access/check", "", &r)
		if r.Check != nil && r.Check.Error == "CHECK_TOO_SOON" {
			time.Sleep(15 * time.Second)
			return false, "CHECK_TOO_SOON"
		}
		return r.Check != nil && r.Check.Error == "" && r.Check.Relay != nil, fmt.Sprintf("%+v", r.Check)
	})
	return r
}

type tryResult struct {
	Name     string `json:"name"`
	OK       bool   `json:"ok"`
	Kind     string `json:"kind"`
	Error    string `json:"error"`
	NodeName string `json:"node_name"`
	MS       int64  `json:"ms"`
}

type device struct {
	Reached string      `json:"reached"`
	Tries   []tryResult `json:"tries"`
}

func (d device) try(name string) tryResult {
	for _, t := range d.Tries {
		if t.Name == name {
			return t
		}
	}
	return tryResult{}
}

// device runs the paired device at home or away, trying home, then the
// router's outside port, then the relay, as the app does.
func (l *lab) device(away bool, direct string) device {
	l.t.Helper()
	// At home the way out is the router, as for a phone on home Wi-Fi;
	// away, the device is on the internet itself.
	network, ip, route := homeNet, homeDev, "ip route replace default via "+routerLAN
	if away {
		network, ip, route = inetNet, awayDev, "ip route del default 2>/dev/null"
	}
	args := []string{"run", "--rm", "--name", prefix + "device", "--network", network, "--ip", ip, "--cap-add", "NET_ADMIN",
		"-v", binDir + ":/opt/bin:ro", image, "sh", "-c", route + `; exec /opt/bin/client "$@"`, "client", "-key", l.key, "-pin", l.pin, "-timeout", "6s",
		"-try", "home=" + l.home}
	if direct != "" {
		args = append(args, "-try", "direct="+direct)
	}
	args = append(args, "-try", fmt.Sprintf("relay=%s:443,%s.%s", relayIP, l.route, relayName))
	out, err := docker(args...)
	if err != nil {
		l.t.Fatal(err)
	}
	var d device
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		l.t.Fatalf("device: %v: %s", err, out)
	}
	return d
}

func (l *lab) eventually(limit time.Duration, what string, ok func() (bool, string)) {
	l.t.Helper()
	deadline := time.Now().Add(limit)
	last := ""
	for time.Now().Before(deadline) {
		var done bool
		if done, last = ok(); done {
			return
		}
		time.Sleep(time.Second)
	}
	l.t.Fatalf("waited %s for %s; last: %s", limit, what, last)
}

// agree asserts the outside check matches what the device could do from
// outside: a way the check calls fine works for the device, and the other
// way around.
func agree(t *testing.T, r reach, d device) {
	t.Helper()
	t.Logf("Core: reachable %s (%s), mapped %q by %q, map reason %q", r.Reachable, r.Reason, r.Mapped, r.MappedBy, r.MapReason)
	t.Logf("check: relay ok=%v %s; direct %+v", r.Check.Relay.OK, r.Check.Relay.Reason, r.Check.Direct)
	for _, tr := range d.Tries {
		t.Logf("device: %-6s ok=%-5v %s %s (%d ms)", tr.Name, tr.OK, tr.Kind, tr.Error, tr.MS)
	}
	if r.Check.Relay.OK != d.try("relay").OK {
		t.Errorf("the check says the relay is ok=%v (%s), but the device's relay try was ok=%v (%s)", r.Check.Relay.OK, r.Check.Relay.Reason, d.try("relay").OK, d.try("relay").Error)
	}
	directOK := false
	for _, w := range r.Check.Direct {
		directOK = directOK || w.OK
	}
	if dt := d.try("direct"); dt.Name != "" && directOK != dt.OK {
		t.Errorf("the check says direct is ok=%v (%+v), but the device's direct try was ok=%v (%s)", directOK, r.Check.Direct, dt.OK, dt.Error)
	}
}

func TestAtHome(t *testing.T) {
	l := newLab(t, router{mode: "none"})
	d := l.device(false, "")
	if d.Reached != "home" || d.try("home").NodeName != "outside-check" {
		t.Fatalf("at home the device should reach the computer on the home network: %+v", d)
	}
}

// Away, with the router opening a port when asked: the device goes direct,
// and Core's check says direct and the relay both work.
func TestAwayPortMapped(t *testing.T) {
	for _, mode := range []string{"pcp", "upnp"} {
		t.Run(mode, func(t *testing.T) {
			l := newLab(t, router{mode: mode})
			l.anywhere(true, "")
			r := l.settled(true)
			if r.Mapped != routerWAN+":7333" {
				t.Fatalf("the router should have opened %s:7333; got %q (%s)", routerWAN, r.Mapped, r.MapError)
			}
			if want := map[string]string{"pcp": "pcp", "upnp": "upnp"}[mode]; r.MappedBy != want {
				t.Errorf("mapped by %q, want %q", r.MappedBy, want)
			}
			if r.Reachable != "direct" {
				t.Errorf("reachable = %q, want direct", r.Reachable)
			}
			r = l.check()
			d := l.device(true, r.Mapped)
			if d.Reached != "direct" {
				t.Errorf("away, the device should reach the computer directly: %+v", d)
			}
			if len(r.Check.Direct) == 0 || !r.Check.Direct[0].OK || !r.Check.Relay.OK {
				t.Errorf("the check should say direct and relay work: %+v %+v", r.Check.Direct, r.Check.Relay)
			}
			agree(t, r, d)
		})
	}
}

// Away, with a port forwarded by hand and its address set.
func TestAwayForwarded(t *testing.T) {
	l := newLab(t, router{mode: "forward"})
	addr := routerWAN + ":7333"
	l.anywhere(false, addr)
	r := l.settled(false)
	if r.Reachable != "manual" {
		t.Errorf("reachable = %q, want manual", r.Reachable)
	}
	r = l.check()
	d := l.device(true, addr)
	if d.Reached != "direct" {
		t.Errorf("away, the device should reach the forwarded port: %+v", d)
	}
	agree(t, r, d)
}

// Away, with a router that opens nothing: only the relay works, Core says
// so, and the check names why direct doesn't (#602).
func TestAwayRelayOnly(t *testing.T) {
	l := newLab(t, router{mode: "none"})
	l.anywhere(true, "")
	r := l.settled(true)
	if r.Reachable != "relay" {
		t.Errorf("reachable = %q, want relay", r.Reachable)
	}
	if r.MapReason == "" {
		t.Errorf("Core should say why the router opened no port: %+v", r)
	}
	r = l.check()
	d := l.device(true, routerWAN+":7333")
	if d.Reached != "relay" {
		t.Errorf("away, the device should come through the relay: %+v", d)
	}
	if !r.Check.Relay.OK {
		t.Errorf("the check should say the relay works: %+v", r.Check.Relay)
	}
	agree(t, r, d)
}

// Away, behind a carrier's NAT: the router's own outside address is the
// carrier's, so it refuses to open a port, as miniupnpd does, and hides the
// address. Only the relay works, and Core says the router has no public
// address instead of that it refused.
func TestAwayCarrierNAT(t *testing.T) {
	for _, mode := range []string{"pcp", "upnp"} {
		t.Run(mode, func(t *testing.T) {
			l := newLab(t, router{mode: mode, carrier: true})
			l.anywhere(true, "")
			r := l.settled(true)
			if r.Reachable != "relay" {
				t.Errorf("reachable = %q, want relay", r.Reachable)
			}
			if r.MapReason != "no_public_address" {
				t.Errorf("map_reason = %q (mapped %q, %q), want no_public_address", r.MapReason, r.Mapped, r.MapError)
			}
			r = l.check()
			d := l.device(true, routerWAN+":7333")
			if d.Reached != "relay" {
				t.Errorf("away, the device should come through the relay: %+v", d)
			}
			agree(t, r, d)
		})
	}
}

// The relay restarts mid-session, as when the cluster ran out of memory
// (2026-10-12): Core gets its tunnel back, and the device comes through
// again, within relayRecover.
func TestRelayRestart(t *testing.T) {
	l := newLab(t, router{mode: "none"})
	l.anywhere(false, "")
	l.settled(false)
	if d := l.device(true, ""); d.Reached != "relay" {
		t.Fatalf("before the restart the device should come through the relay: %+v", d)
	}
	// The tunnel has to have been up a while, as in production, for Core's
	// backoff to start over.
	time.Sleep(65 * time.Second)
	start := time.Now()
	if _, err := docker("restart", "-t", "0", prefix+"relay"); err != nil {
		t.Fatal(err)
	}
	var d device
	last := ""
	l.eventually(relayRecover, "the device to come through the relay again", func() (bool, string) {
		if r := l.reach(); r.Relay != nil && r.Relay.State+r.Relay.Error != last {
			last = r.Relay.State + r.Relay.Error
			t.Logf("%s Core's relay state: %s %s", time.Since(start).Round(time.Second), r.Relay.State, r.Relay.Error)
		}
		d = l.device(true, "")
		return d.Reached == "relay", fmt.Sprintf("%+v", d)
	})
	t.Logf("the device came through the relay again %s after it restarted", time.Since(start).Round(time.Second))
}

// The computer's certificate changes, as after reinstalling: the device
// that paired with the old one is refused by its pin on every way, which
// the app shows as pair again, not as can't reach.
func TestNewCertificate(t *testing.T) {
	l := newLab(t, router{mode: "none"})
	l.anywhere(false, "")
	l.settled(false)
	if _, err := docker("exec", prefix+"core", "sh", "-c", "rm /data/secrets/api-tls.crt /data/secrets/api-tls.key"); err != nil {
		t.Fatal(err)
	}
	if _, err := docker("restart", prefix+"core"); err != nil {
		t.Fatal(err)
	}
	l.eventually(30*time.Second, "Core's API after the restart", func() (bool, string) {
		_, err := l.api("GET", "/api/v1/health", "")
		return err == nil, fmt.Sprint(err)
	})
	l.settled(false)
	for _, away := range []bool{false, true} {
		d := l.device(away, "")
		if d.Reached != "" {
			t.Fatalf("a device paired with the old certificate should reach nothing: %+v", d)
		}
		for _, tr := range d.Tries {
			if tr.Name == "home" && away {
				continue // not on the home network
			}
			if tr.Kind != "pin" {
				t.Errorf("away=%v %s: kind %q (%s), want pin: the device must be able to tell it should pair again", away, tr.Name, tr.Kind, tr.Error)
			}
		}
	}
}
