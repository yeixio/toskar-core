package connectors

import (
	"context"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// reolink-cli reaches cameras through its gateway, a program of its own
// that `reolink-cli gateway start` runs in the foreground. One that has run
// for a long time can stop reaching the camera while it still answers
// (#667): every login fails with "No route to host" until it's restarted.
// So for a gateway on this computer, Toskar starts it when nothing is
// listening, and restarts it once when reolink-cli says the camera can't be
// reached, then tries again.

var gateway struct {
	mu  sync.Mutex
	cmd *exec.Cmd
}

// gatewayUp reports something listening at addr. Tests replace it.
var gatewayUp = func(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// localGateway reports a gateway address on this computer, which Toskar
// may start; one elsewhere is left to whoever runs it.
func localGateway(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// unreachable reports reolink-cli failing to reach the camera, as a stale
// gateway makes it.
func unreachable(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unreachable") || strings.Contains(s, "network_error") || strings.Contains(s, "no route to host")
}

// ensureGateway starts the gateway at addr when nothing listens there, or,
// with restart, stops whatever gateway runs and starts it again. It waits
// up to five seconds for it to listen.
func ensureGateway(ctx context.Context, bin, addr string, restart bool) {
	if addr == "" || !localGateway(addr) {
		return
	}
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	if !restart && gatewayUp(addr) {
		return
	}
	if restart {
		stop, cancel := context.WithTimeout(ctx, 5*time.Second)
		// Only the gateway at addr: another copy of Toskar, or one the
		// person runs, may use another (#1340).
		_ = exec.CommandContext(stop, bin, "--gateway-addr", addr, "gateway", "stop").Run()
		cancel()
		if gateway.cmd != nil && gateway.cmd.Process != nil {
			_ = gateway.cmd.Process.Kill()
		}
		gateway.cmd = nil
	}
	// It outlives the request that needed it, and runs until Toskar stops
	// or it's restarted.
	cmd := exec.Command(bin, "gateway", "start", "--addr", addr)
	if err := cmd.Start(); err != nil {
		return
	}
	gateway.cmd = cmd
	go func() { _ = cmd.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		if gatewayUp(addr) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}
