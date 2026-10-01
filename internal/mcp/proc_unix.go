//go:build !windows

package mcp

import (
	"os/exec"
	"syscall"
)

// hideWindow puts the server in its own process group, so stopping it also
// stops what it started, such as the node process npx runs.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminate(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
}
