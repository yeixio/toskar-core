//go:build windows

package llamacpp

import (
	"os"
	"os/exec"
	"strconv"
	"time"
)

func configureLlamaProcess(cmd *exec.Cmd) {}

// stopLlamaProcess stops llama-server. exited, when the supervisor's watch
// waits for the process, is closed when it ends; a second Wait would race
// with that one.
func stopLlamaProcess(cmd *exec.Cmd, exited <-chan struct{}, graceful time.Duration) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Signal(os.Interrupt)
	done := exited
	if done == nil {
		ch := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(ch)
		}()
		done = ch
	}
	select {
	case <-done:
		return
	case <-time.After(graceful):
	}
	_ = exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").Run()
	select {
	case <-done:
	case <-time.After(800 * time.Millisecond):
	}
}
