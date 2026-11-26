package connectors

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yeixio/toskar-core/internal/connectors/reolinktest"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A gateway on this computer that has gone stale is restarted, and the
// words are said again; one that isn't running is started first; one on
// another computer is left alone (#667).
func TestReolinkRestartsAStaleGateway(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake reolink-cli is a shell script")
	}
	dir := t.TempDir()
	// Talking fails until the gateway has been started again.
	script := "#!/bin/sh\necho \"$*\" >> \"" + dir + "/calls\"\n" +
		"case \"$*\" in\n" +
		"  'gateway start'*) touch \"" + dir + "/fresh\"; exit 0;;\n" +
		"  *'gateway stop') touch \"" + dir + "/stopped\"; exit 0;;\n" +
		"esac\n" +
		"cat > /dev/null\n" +
		"if [ -f \"" + dir + "/fresh\" ]; then echo '{\"ok\":true}'; else echo '{\"ok\":false,\"error\":{\"code\":\"NETWORK_ERROR\",\"message\":\"device unreachable: No route to host (os error 65)\"}}'; fi\n"
	bin := filepath.Join(dir, "reolink-cli")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	var up atomic.Bool
	up.Store(true)
	oldCLI, oldUp := reolinkCLI, gatewayUp
	reolinkCLI = func() (string, error) { return bin, nil }
	exists := func(name string) bool { _, err := os.Stat(filepath.Join(dir, name)); return err == nil }
	// A stopped gateway listens again once it's started.
	gatewayUp = func(string) bool { return up.Load() && (!exists("stopped") || exists("fresh")) }
	t.Cleanup(func() { reolinkCLI, gatewayUp = oldCLI, oldUp })

	door := reolinktest.New(false)
	defer door.Close()
	m := reolinkManager(t)
	ctx := context.Background()
	connect := func(gw string) {
		if _, err := m.Connect(ctx, "reolink", map[string]string{"url": door.URL, "username": "admin", "password": door.Password, "talk_camera": "hub-ch0", "talk_gateway": gw}); err != nil {
			t.Fatal(err)
		}
	}
	calls := func() string {
		b, _ := os.ReadFile(filepath.Join(dir, "calls"))
		_ = os.Remove(filepath.Join(dir, "calls"))
		return strings.TrimSpace(string(b))
	}
	words := make([]int16, ReolinkTalkRate/10)

	connect("192.168.1.9:9000")
	err := m.Talk(ctx, "reolink", "camera", words, ReolinkTalkRate)
	// Regression: toskar-core#1340
	if code, _ := contracts.ErrorCode(err); code != "CAMERA_SPEAKER_UNREACHABLE" || !strings.Contains(err.Error(), "No route to host") || strings.Contains(err.Error(), "{") {
		t.Fatalf("a gateway elsewhere: %q %v", code, err)
	}
	if c := calls(); strings.Contains(c, "gateway st") {
		t.Fatalf("restarted a gateway on another computer: %q", c)
	}

	connect("127.0.0.1:9000")
	if err := m.Talk(ctx, "reolink", "camera", words, ReolinkTalkRate); err != nil {
		t.Fatal(err)
	}
	want := "--gateway-addr 127.0.0.1:9000 --camera hub-ch0 audio talk --stdin --sample-rate 16000\n--gateway-addr 127.0.0.1:9000 gateway stop\ngateway start --addr 127.0.0.1:9000\n" +
		"--gateway-addr 127.0.0.1:9000 --camera hub-ch0 audio talk --stdin --sample-rate 16000"
	if c := calls(); c != want {
		t.Fatalf("calls:\n%s", c)
	}

	// Nothing listening: it's started before talking.
	up.Store(false)
	go func() {
		for !strings.Contains(string(must(os.ReadFile(filepath.Join(dir, "calls")))), "gateway start") {
		}
		up.Store(true)
	}()
	if err := m.Talk(ctx, "reolink", "camera", words, ReolinkTalkRate); err != nil {
		t.Fatal(err)
	}
	if c := calls(); !strings.HasPrefix(c, "gateway start --addr 127.0.0.1:9000\n--gateway-addr") {
		t.Fatalf("calls:\n%s", c)
	}
}

func must(b []byte, _ error) []byte { return b }
