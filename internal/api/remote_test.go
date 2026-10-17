package api

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// The remote listener serves paired devices' keys on phone routes, over
// TLS, and nothing else: no "this computer", no other keys, no pages, and
// an address that keeps sending wrong keys waits (#456).
func TestRemoteListener(t *testing.T) {
	dir := t.TempDir()
	mgr, err := config.NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := auth.APICertificate(auth.NewSecretStore(dir))
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Dependencies{
		Config: mgr,
		VerifyAPIKey: func(_ context.Context, secret string) (auth.APIKeyRecord, error) {
			switch secret {
			case "phone-key":
				return auth.APIKeyRecord{ID: "d1", PersonID: auth.OwnerID, Kind: auth.KindDevice}, nil
			case "api-key":
				return auth.APIKeyRecord{ID: "k1", PersonID: auth.OwnerID}, nil
			}
			return auth.APIKeyRecord{}, errors.New("invalid")
		},
	})
	srv.SetTLS(&tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, APITLS{Enabled: true})
	if err := srv.ServeRemote("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.StopRemote)
	t.Cleanup(func() { remoteFailures.Forget("127.0.0.1") })
	base := "https://" + srv.RemoteListening().Listening
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	call := func(method, path, key string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(method, base+path, strings.NewReader("{}"))
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var b strings.Builder
		buf := make([]byte, 2048)
		n, _ := res.Body.Read(buf)
		b.Write(buf[:n])
		return res.StatusCode, b.String()
	}

	if code, _ := call(http.MethodGet, "/api/v1/health", "phone-key"); code != http.StatusOK {
		t.Fatalf("a phone's key: %d", code)
	}
	if code, body := call(http.MethodGet, "/api/v1/api-keys", "phone-key"); code != http.StatusForbidden || !strings.Contains(body, "DEVICE_NOT_ALLOWED") {
		t.Fatalf("a phone's key off its routes: %d %s", code, body)
	}
	if code, body := call(http.MethodGet, "/api/v1/health", "api-key"); code != http.StatusForbidden || !strings.Contains(body, "REMOTE_DEVICES_ONLY") {
		t.Fatalf("an API Access key: %d %s", code, body)
	}
	// From this very computer, with no key: still nobody.
	if code, _ := call(http.MethodGet, "/api/v1/settings", ""); code != http.StatusUnauthorized {
		t.Fatalf("no key: %d", code)
	}
	if code, _ := call(http.MethodPost, "/api/v1/devices/pair", ""); code != http.StatusUnauthorized {
		t.Fatalf("pairing from outside: %d", code)
	}
	if code, _ := call(http.MethodGet, "/", "phone-key"); code != http.StatusNotFound {
		t.Fatalf("pages: %d", code)
	}
	// Wrong keys: twenty, then the address waits, even with a good key.
	remoteFailures.Forget("127.0.0.1")
	for i := 0; i < 20; i++ {
		call(http.MethodGet, "/api/v1/health", "guess")
	}
	if code, body := call(http.MethodGet, "/api/v1/health", "phone-key"); code != http.StatusTooManyRequests || !strings.Contains(body, "REMOTE_THROTTLED") {
		t.Fatalf("after wrong keys: %d %s", code, body)
	}
	remoteFailures.Forget("127.0.0.1")

	// Off closes it.
	addr := srv.RemoteListening().Listening
	srv.StopRemote()
	if srv.RemoteListening().Listening != "" {
		t.Fatal("still listening")
	}
	if _, err := client.Get("https://" + addr + "/api/v1/health"); err == nil {
		t.Fatal("answered after it was turned off")
	}
}

// Without a certificate there's nothing to listen with.
func TestRemoteListenerNeedsTLS(t *testing.T) {
	srv := NewServer(Dependencies{})
	if err := srv.ServeRemote("127.0.0.1:0"); err == nil || srv.RemoteListening().Error == "" {
		t.Fatalf("listened without a certificate: %v", err)
	}
}

// The route secret comes only on the home network, never through the
// remote listener, and with pairing (#456).
func TestRouteSecret(t *testing.T) {
	dir := t.TempDir()
	mgr, err := config.NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Update(func(c *config.Config) { c.APIHost, c.LANAPIEnabled = "0.0.0.0", true }); err != nil {
		t.Fatal(err)
	}
	cert, err := auth.APICertificate(auth.NewSecretStore(dir))
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Dependencies{
		Config: mgr,
		VerifyAPIKey: func(_ context.Context, secret string) (auth.APIKeyRecord, error) {
			if secret == "phone-key" {
				return auth.APIKeyRecord{ID: "d1", PersonID: auth.OwnerID, Kind: auth.KindDevice}, nil
			}
			return auth.APIKeyRecord{}, errors.New("invalid")
		},
		RouteSecret: func() (string, string, error) { return "c2VjcmV0", "routeid", nil },
		RelayName:   func() string { return "relay.example.com" },
		RelayToken:  func() string { return "rt1.token" },
	})
	srv.SetTLS(&tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, APITLS{Enabled: true})

	// On the home network, with a phone's key.
	r := httptest.NewRequest(http.MethodGet, "/api/v1/remote-access/route", nil)
	r.RemoteAddr, r.Host = "192.168.1.20:50000", "192.168.1.5:7331"
	r.Header.Set("Authorization", "Bearer phone-key")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"route_secret":"c2VjcmV0"`) || !strings.Contains(rec.Body.String(), `"relay":"relay.example.com"`) {
		t.Fatalf("home network: %d %s", rec.Code, rec.Body)
	}
	// The computer's relay token, so this device can find it away too.
	if !strings.Contains(rec.Body.String(), `"token":"rt1.token"`) {
		t.Fatalf("no relay token for the device: %s", rec.Body)
	}

	// Through the remote listener: refused, though the key is good.
	if err := srv.ServeRemote("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.StopRemote)
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	req, _ := http.NewRequest(http.MethodGet, "https://"+srv.RemoteListening().Listening+"/api/v1/remote-access/route", nil)
	req.Header.Set("Authorization", "Bearer phone-key")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("remote: %d", res.StatusCode)
	}
}

// pipeConn is a device's connection as the relay client hands it over:
// its address is the device's, not the relay's.
type pipeConn struct {
	net.Conn
	remote net.Addr
}

func (c pipeConn) RemoteAddr() net.Addr { return c.remote }

// A device through the relay meets the remote listener as a direct one
// does: TLS with the API's certificate, paired devices' keys only, wrong
// keys counted against the device's address (#456).
func TestServeRelayed(t *testing.T) {
	dir := t.TempDir()
	mgr, err := config.NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := auth.APICertificate(auth.NewSecretStore(dir))
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Dependencies{
		Config: mgr,
		VerifyAPIKey: func(_ context.Context, secret string) (auth.APIKeyRecord, error) {
			if secret == "phone-key" {
				return auth.APIKeyRecord{ID: "d1", PersonID: auth.OwnerID, Kind: auth.KindDevice}, nil
			}
			return auth.APIKeyRecord{}, errors.New("invalid")
		},
	})
	if srv.ServeRelayed(nil) {
		t.Fatal("took a connection with the listener off")
	}
	srv.SetTLS(&tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, APITLS{Enabled: true})
	if err := srv.ServeRemote("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.StopRemote)
	device := "203.0.113.50"
	t.Cleanup(func() { remoteFailures.Forget(device) })
	pin := sha256.Sum256(cert.Certificate[0])
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			a, b := net.Pipe()
			if !srv.ServeRelayed(pipeConn{Conn: b, remote: &net.TCPAddr{IP: net.ParseIP(device)}}) {
				return nil, errors.New("not taken")
			}
			return a, nil
		},
		// The device trusts the pinned certificate, as through the relay.
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true, VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if sha256.Sum256(raw[0]) != pin {
				return errors.New("not the pinned certificate")
			}
			return nil
		}},
	}}
	call := func(key string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, "https://relayed.invalid/api/v1/health", nil)
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var body struct {
			Error struct{ Code string }
		}
		_ = json.NewDecoder(res.Body).Decode(&body)
		return res.StatusCode, body.Error.Code
	}
	if status, _ := call("phone-key"); status != http.StatusOK {
		t.Fatalf("phone key: %d", status)
	}
	if status, code := call(""); status != http.StatusUnauthorized {
		t.Fatalf("no key: %d %s", status, code)
	}
	for range 20 {
		call("wrong")
	}
	if status, code := call("phone-key"); status != http.StatusTooManyRequests || code != "REMOTE_THROTTLED" {
		t.Fatalf("after wrong keys from the device's address: %d %s", status, code)
	}

	// Through a relay that hides client addresses, devices have none:
	// wrong keys there aren't pooled into one limit for everyone.
	device = "0.0.0.0"
	client.CloseIdleConnections()
	for range 25 {
		call("wrong")
	}
	if status, code := call("phone-key"); status != http.StatusOK {
		t.Fatalf("a relayed device with no address after others' wrong keys: %d %s", status, code)
	}
	srv.StopRemote()
	if srv.ServeRelayed(pipeConn{remote: &net.TCPAddr{}}) {
		t.Fatal("took a connection after stopping")
	}
}

// The app hands over a subscription's relay token; turning access from
// anywhere on with it needs an Admin (#456).
func TestRelayToken(t *testing.T) {
	var got []string
	srv := NewServer(Dependencies{
		SetRelayToken: func(_ context.Context, tok string, enable bool) (string, error) {
			if tok == "bad" {
				return "", contracts.NewError("RELAY_TOKEN_INVALID", nil, errors.New("not for this computer"))
			}
			got = append(got, fmt.Sprintf("%s %v", tok, enable))
			return "connecting", nil
		},
	})
	call := func(role auth.Role, body string) (int, string) {
		r := httptest.NewRequest(http.MethodPut, "/api/v1/remote-access/relay-token", strings.NewReader(body))
		r = r.WithContext(auth.WithPrincipal(r.Context(), auth.Principal{Person: auth.Person{ID: "p", Role: role}}))
		w := httptest.NewRecorder()
		srv.handleRelayToken(w, r)
		var out struct {
			State string
			Error struct{ Code string }
		}
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out.State + out.Error.Code
	}
	if status, code := call(auth.RoleMember, `{"token":"rt1.x.y","enable":true}`); status != http.StatusForbidden || code != "ROLE_REQUIRED" {
		t.Fatalf("member enabling: %d %s", status, code)
	}
	if status, state := call(auth.RoleMember, `{"token":"rt1.x.y"}`); status != http.StatusOK || state != "connecting" {
		t.Fatalf("member handing over: %d %s", status, state)
	}
	if status, _ := call(auth.RoleAdmin, `{"token":"rt1.x.y","enable":true}`); status != http.StatusOK {
		t.Fatalf("admin enabling: %d", status)
	}
	if status, code := call(auth.RoleAdmin, `{"token":"bad"}`); status != http.StatusBadRequest || code != "RELAY_TOKEN_INVALID" {
		t.Fatalf("bad token: %d %s", status, code)
	}
	if status, code := call(auth.RoleAdmin, `nope`); status != http.StatusBadRequest || code != "INVALID_JSON" {
		t.Fatalf("bad JSON: %d %s", status, code)
	}
	if len(got) != 2 || got[0] != "rt1.x.y false" || got[1] != "rt1.x.y true" {
		t.Fatalf("handed over: %v", got)
	}
	if !auth.DeviceMayReach(http.MethodPut, "/api/v1/remote-access/relay-token") {
		t.Fatal("a paired device can't hand over a token")
	}
}

// The outside check (#546): Check now runs it and answers the same as
// GET /remote-access, with the result under check.
func TestRemoteCheck(t *testing.T) {
	ran := 0
	on := true
	at := time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC)
	srv := NewServer(Dependencies{
		RemoteReach: func() RemoteReach {
			r := RemoteReach{Reachable: "relay", Relay: "relay.toskar.ai:4443", RelayState: "connected"}
			if ran > 0 {
				r.Check = &ReachCheck{At: at, Tried: at, Relay: &ReachWay{OK: true, MS: 80},
					Direct: []ReachWay{{Address: "198.51.100.7:7333", Reason: "timeout", MS: 8000}}}
			}
			return r
		},
		CheckReach: func(context.Context) error {
			if !on {
				return contracts.NewError("REMOTE_ACCESS_OFF", nil, errors.New("off"))
			}
			ran++
			return nil
		},
	})
	call := func() (int, map[string]any) {
		w := httptest.NewRecorder()
		srv.handleRemoteCheck(w, httptest.NewRequest(http.MethodPost, "/api/v1/remote-access/check", nil))
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	status, out := call()
	if status != http.StatusOK || ran != 1 {
		t.Fatalf("check: %d ran %d", status, ran)
	}
	check, _ := out["check"].(map[string]any)
	relay, _ := check["relay"].(map[string]any)
	direct, _ := check["direct"].([]any)
	if relay["ok"] != true || len(direct) != 1 || direct[0].(map[string]any)["reason"] != "timeout" || check["at"] != "2026-10-12T18:00:00Z" {
		t.Fatalf("answer: %v", out)
	}
	on = false
	if status, out := call(); status != http.StatusConflict || out["error"].(map[string]any)["code"] != "REMOTE_ACCESS_OFF" {
		t.Fatalf("off: %d %v", status, out)
	}
	// Admins only, from the web or an Admin's phone (#546): a device key
	// may ask, and its person's role still has to be Admin.
	if !auth.DeviceMayReach(http.MethodPost, "/api/v1/remote-access/check") || auth.RoleNeeded(http.MethodPost, "/api/v1/remote-access/check") != auth.RoleAdmin {
		t.Fatal("the check is for Admins, from any of their devices")
	}
}

// Subscribing in Settings (#639): an Admin's, never a paired device's, and
// each failure with its own status.
func TestSubscriptionRoutes(t *testing.T) {
	var fail error
	srv := NewServer(Dependencies{
		StartCheckout: func(_ context.Context, plan string) (string, error) {
			if fail != nil {
				return "", fail
			}
			return "https://checkout.stripe.test/" + plan, nil
		},
		ManageSubscription: func(context.Context) (string, error) {
			if fail != nil {
				return "", fail
			}
			return "https://billing.stripe.test/s", nil
		},
		Subscription: func() *Subscription {
			return &Subscription{State: "pending", Store: "stripe", Checkout: "https://checkout.stripe.test/x"}
		},
	})
	call := func(handler http.HandlerFunc, body string) (int, string) {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		w := httptest.NewRecorder()
		handler(w, r)
		var out struct {
			URL   string
			Error struct{ Code string }
		}
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out.URL + out.Error.Code
	}
	if status, url := call(srv.handleSubscriptionCheckout, `{"plan":"yearly"}`); status != http.StatusOK || url != "https://checkout.stripe.test/yearly" {
		t.Fatalf("checkout: %d %s", status, url)
	}
	if status, url := call(srv.handleSubscriptionManage, ``); status != http.StatusOK || url != "https://billing.stripe.test/s" {
		t.Fatalf("manage: %d %s", status, url)
	}
	for code, want := range map[string]int{
		"SUBSCRIPTION_PLAN_INVALID": http.StatusBadRequest,
		"SUBSCRIPTION_OWN_RELAY":    http.StatusConflict,
		"SUBSCRIPTION_ACTIVE":       http.StatusConflict,
		"SUBSCRIPTION_IN_APP_ONLY":  http.StatusConflict,
		"SUBSCRIPTION_NONE":         http.StatusNotFound,
		"RELAY_SOLD_OUT":            http.StatusServiceUnavailable,
		"BILLING_UNAVAILABLE":       http.StatusBadGateway,
	} {
		fail = contracts.NewError(code, nil, errors.New(code))
		if status, got := call(srv.handleSubscriptionCheckout, `{"plan":"monthly"}`); status != want || got != code {
			t.Errorf("%s: %d %s", code, status, got)
		}
	}
	fail = errors.New("disk full")
	if status, got := call(srv.handleSubscriptionManage, ``); status != http.StatusInternalServerError || got != "SUBSCRIPTION_FAILED" {
		t.Fatalf("other: %d %s", status, got)
	}
	if status, got := call(srv.handleSubscriptionCheckout, `nope`); status != http.StatusBadRequest || got != "INVALID_JSON" {
		t.Fatalf("bad JSON: %d %s", status, got)
	}
	for _, route := range []string{"/api/v1/remote-access/subscription/checkout", "/api/v1/remote-access/subscription/manage"} {
		if auth.RoleNeeded(http.MethodPost, route) != auth.RoleAdmin || auth.DeviceMayReach(http.MethodPost, route) {
			t.Errorf("%s isn't an Admin's alone", route)
		}
	}
	// GET /remote-access says how it stands.
	w := httptest.NewRecorder()
	srv.handleRemoteAccess(w, httptest.NewRequest(http.MethodGet, "/api/v1/remote-access", nil))
	if !strings.Contains(w.Body.String(), `"subscription":{"state":"pending","store":"stripe","checkout":"https://checkout.stripe.test/x"}`) {
		t.Fatalf("status: %s", w.Body.String())
	}
}
