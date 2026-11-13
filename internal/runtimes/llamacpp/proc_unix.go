//go:build unix

package llamacpp

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

func configureLlamaProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// stopLlamaProcess stops llama-server. exited, when the supervisor's watch
// waits for the process, is closed when it ends; a second Wait would race
// with that one.
func stopLlamaProcess(cmd *exec.Cmd, exited <-chan struct{}, graceful time.Duration) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if err := syscall.Kill(-pid, syscall.SIGINT); err != nil {
		_ = cmd.Process.Signal(os.Interrupt)
	}
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
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	_ = syscall.Kill(pid, syscall.SIGKILL)
	select {
	case <-done:
	case <-time.After(800 * time.Millisecond):
	}
}
