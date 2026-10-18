package app

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/config"
)

func TestStartRefusesNonLoopbackWithoutAPIKey(t *testing.T) {
	t.Setenv("YGGDRASIL_API_HOST", "0.0.0.0")
	t.Setenv("YGGDRASIL_DISCOVERY_ENABLED", "false")
	application, err := New(Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Shutdown(context.Background()) })
	err = application.Start(context.Background())
	if !errors.Is(err, auth.ErrAPIKeyRequired) {
		t.Fatalf("start: %v", err)
	}
}

func TestLANEnableRequiresAPIKey(t *testing.T) {
	t.Setenv("YGGDRASIL_API_HOST", "127.0.0.1")
	t.Setenv("YGGDRASIL_DISCOVERY_ENABLED", "false")
	application, err := New(Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Shutdown(context.Background()) })
	err = application.applySettingsPatch(context.Background(), map[string]any{"lan_api_enabled": true})
	if !errors.Is(err, auth.ErrAPIKeyRequired) {
		t.Fatalf("lan enable: %v", err)
	}

	// A person added to sign in from other devices is enough (#206): their
	// link, and then their password, need the network.
	sam, err := application.People.Create(context.Background(), "Sam", auth.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if err := application.applySettingsPatch(context.Background(), map[string]any{"lan_api_enabled": true}); err != nil {
		t.Fatalf("lan enable with a person: %v", err)
	}
	if err := application.applySettingsPatch(context.Background(), map[string]any{"lan_api_enabled": false}); err != nil {
		t.Fatal(err)
	}
	disabled := true
	if _, err := application.People.Update(context.Background(), sam.ID, auth.Change{Disabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	err = application.applySettingsPatch(context.Background(), map[string]any{"lan_api_enabled": true})
	if !errors.Is(err, auth.ErrAPIKeyRequired) {
		t.Fatalf("lan enable with only a disabled person: %v", err)
	}
}

func TestStartAcceptsBootstrapKey(t *testing.T) {
	apiPort := freePort(t)
	internalPort := freePort(t)
	const secret = "ygg_bootstrap_test_key"
	t.Setenv("YGGDRASIL_API_HOST", "0.0.0.0")
	t.Setenv("YGGDRASIL_API_PORT", strconv.Itoa(apiPort))
	t.Setenv("YGGDRASIL_INTERNAL_HOST", "127.0.0.1")
	t.Setenv("YGGDRASIL_INTERNAL_PORT", strconv.Itoa(internalPort))
	t.Setenv("YGGDRASIL_DISCOVERY_ENABLED", "false")
	t.Setenv("YGGDRASIL_API_KEY", secret)

	application, err := New(Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- application.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = application.Shutdown(context.Background())
	})

	// The test reaches the daemon over loopback, which needs no key even
	// with network access on; a forwarding header stands in for a request
	// from another machine.
	base := "http://127.0.0.1:" + strconv.Itoa(apiPort)
	client := &http.Client{Timeout: 2 * time.Second}
	get := func(forwarded bool, key string) int {
		req, err := http.NewRequest(http.MethodGet, base+"/api/v1/health", nil)
		if err != nil {
			t.Fatal(err)
		}
		if forwarded {
			req.Header.Set("X-Forwarded-For", "192.168.1.20")
		}
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	deadline := time.Now().Add(10 * time.Second)
	for get(false, "") != http.StatusOK {
		select {
		case startErr := <-errCh:
			t.Fatalf("start: %v", startErr)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("health from this computer without a key never answered")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if code := get(true, ""); code != http.StatusUnauthorized {
		t.Fatalf("health from the network without a key: %d", code)
	}
	if code := get(true, secret); code != http.StatusOK {
		t.Fatalf("bearer health: %d", code)
	}
	if host := application.Config.Get().APIHost; !config.ListensBeyondLoopback(host) {
		t.Fatalf("api host %q", host)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// Network access turned on in the app with no key or phone ever using it
// goes back to this computer only at the next start, instead of refusing to
// start (#216).
func TestStartTurnsOffUnusedLANAccess(t *testing.T) {
	for _, prefix := range []string{"YGGDRASIL_", "TOSKAR_"} {
		t.Setenv(prefix+"API_HOST", "")
	}
	t.Setenv("YGGDRASIL_API_PORT", strconv.Itoa(freePort(t)))
	t.Setenv("YGGDRASIL_INTERNAL_HOST", "127.0.0.1")
	t.Setenv("YGGDRASIL_INTERNAL_PORT", strconv.Itoa(freePort(t)))
	t.Setenv("YGGDRASIL_DISCOVERY_ENABLED", "false")
	application, err := New(Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.Config.Update(func(c *config.Config) { c.LANAPIEnabled, c.APIHost = true, "0.0.0.0" }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- application.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		_ = application.Shutdown(context.Background())
	})
	deadline := time.Now().Add(10 * time.Second)
	for application.API.Addr() == "" && time.Now().Before(deadline) {
		select {
		case err := <-errCh:
			t.Fatalf("start: %v", err)
		case <-time.After(20 * time.Millisecond):
		}
	}
	cfg := application.Config.Get()
	if cfg.LANAPIEnabled || config.ListensBeyondLoopback(cfg.APIHost) {
		t.Fatalf("config = lan %v host %q", cfg.LANAPIEnabled, cfg.APIHost)
	}
	if host, _, _ := net.SplitHostPort(application.API.Addr()); host != "127.0.0.1" {
		t.Fatalf("listening on %s", application.API.Addr())
	}

	// Connect a device turns it on again, with no key yet, at once.
	if err := application.enableLANForPhone(context.Background()); err != nil {
		t.Fatal(err)
	}
	if host, _, _ := net.SplitHostPort(application.API.Addr()); host != "0.0.0.0" && host != "::" {
		t.Fatalf("after enabling, listening on %s", application.API.Addr())
	}
	if !application.Config.Get().LANAPIEnabled {
		t.Fatal("the setting didn't change")
	}
}

// Settings say who could connect from another computer, for Admins only
// (#711): people sign in with a password, devices with a code, apps with
// an API key.
func TestSettingsSayWhoCanConnect(t *testing.T) {
	t.Setenv("YGGDRASIL_API_HOST", "127.0.0.1")
	t.Setenv("YGGDRASIL_DISCOVERY_ENABLED", "false")
	application, err := New(Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Shutdown(context.Background()) })
	ctx := context.Background()

	view, err := application.settingsView(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if view.NetworkAccess == nil || view.NetworkAccess.Anyone() {
		t.Fatalf("a new install: %+v", view.NetworkAccess)
	}

	if _, err := application.People.Create(ctx, "Sam", auth.RoleMember); err != nil {
		t.Fatal(err)
	}
	if _, _, err := application.APIKeys.CreateDevice(ctx, "Sam's phone"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := application.APIKeys.Create(ctx, "Script"); err != nil {
		t.Fatal(err)
	}
	view, err = application.settingsView(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n := view.NetworkAccess; n == nil || n.People != 1 || n.Devices != 1 || n.Keys != 1 || n.SignInService {
		t.Fatalf("a person, a device, and a key: %+v", n)
	}

	member := auth.WithPrincipal(ctx, auth.Principal{Person: auth.Person{ID: "sam", Role: auth.RoleMember}})
	view, err = application.settingsView(member)
	if err != nil {
		t.Fatal(err)
	}
	if view.NetworkAccess != nil {
		t.Fatalf("a Member sees who can connect: %+v", view.NetworkAccess)
	}
}
