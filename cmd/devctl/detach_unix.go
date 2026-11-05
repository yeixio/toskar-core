//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detach gives Toskar its own session, so it goes on after this terminal and
// Ctrl-C here doesn't reach it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
