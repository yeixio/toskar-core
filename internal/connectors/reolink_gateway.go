package connectors

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	mu     sync.Mutex
	cmd    *exec.Cmd
	failed error // why it last didn't start, said with the visit's error
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
	cmd := gatewayCommand(bin, addr)
	out := &tailBuffer{max: 2048}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		gateway.failed = fmt.Errorf("couldn't start reolink-cli's gateway: %w", err)
		slog.Warn("reolink gateway didn't start", "addr", addr, "command", cmd.Path, "error", err)
		return
	}
	gateway.cmd = cmd
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		if gatewayUp(addr) {
			gateway.failed = nil
			return
		}
		select {
		case err := <-exited:
			// It stopped at once, as it does when it can't run: say why,
			// in the log and in what the visit reports (#1498).
			gateway.cmd = nil
			gateway.failed = gatewayExited(cmd.Path, err, out.String())
			slog.Warn("reolink gateway stopped as it started", "addr", addr, "command", cmd.Path, "error", err, "output", out.String())
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	if ctx.Err() == nil {
		gateway.failed = fmt.Errorf("reolink-cli's gateway didn't start listening at %s within 5 seconds%s", addr, outputNote(out.String()))
		slog.Warn("reolink gateway isn't listening", "addr", addr, "command", cmd.Path, "output", out.String())
	}
}

// gatewayCommand starts the gateway at addr. reolink-cli's `gateway start`
// runs reolink-gateway, which its installer puts beside it; a service such
// as Toskar's on Linux runs with a PATH that has neither in it (#1498). So
// Toskar runs reolink-gateway itself when it's beside reolink-cli or on the
// PATH, and otherwise asks reolink-cli, with reolink-cli's folder on the
// PATH either way.
func gatewayCommand(bin, addr string) *exec.Cmd {
	dir := filepath.Dir(bin)
	var cmd *exec.Cmd
	if gw := gatewayBinary(dir); gw != "" {
		cmd = exec.Command(gw, addr)
	} else {
		cmd = exec.Command(bin, "gateway", "start", "--addr", addr)
	}
	cmd.Env = withPath(os.Environ(), dir)
	return cmd
}

// gatewayBinary finds reolink-gateway beside reolink-cli, then on the PATH.
func gatewayBinary(dir string) string {
	name := "reolink-gateway"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if p := filepath.Join(dir, name); isFile(p) {
		return p
	}
	if p, err := exec.LookPath("reolink-gateway"); err == nil {
		return p
	}
	return ""
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// withPath puts dir first on env's PATH.
func withPath(env []string, dir string) []string {
	out := make([]string, 0, len(env)+1)
	found := false
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.EqualFold(k, "PATH") && !found {
			found = true
			if v == "" {
				kv = k + "=" + dir
			} else {
				kv = k + "=" + dir + string(os.PathListSeparator) + v
			}
		}
		out = append(out, kv)
	}
	if !found {
		out = append(out, "PATH="+dir)
	}
	return out
}

// gatewayExited says why the gateway stopped as it started.
func gatewayExited(path string, err error, output string) error {
	why := "it stopped"
	if err != nil {
		why = "it stopped (" + err.Error() + ")"
	}
	return fmt.Errorf("Toskar started reolink-cli's gateway (%s) but %s%s", path, why, outputNote(output))
}

func outputNote(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	if i := strings.LastIndexByte(output, '\n'); i >= 0 && len(output)-i > 8 {
		output = output[i+1:]
	}
	return ": " + output
}

// gatewayFailure is why Toskar last couldn't start the gateway, or nil.
func gatewayFailure() error {
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	return gateway.failed
}

// notRunning reports reolink-cli finding no gateway listening.
func notRunning(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "gateway connect failed") || strings.Contains(s, "gateway daemon isn't running")
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	b   []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > t.max {
		t.b = append([]byte(nil), t.b[len(t.b)-t.max:]...)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.b)
}
