//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// detach keeps Toskar off this console, so it goes on after this terminal and
// Ctrl-C here doesn't reach it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x00000008 | 0x00000200} // DETACHED_PROCESS, CREATE_NEW_PROCESS_GROUP
}
