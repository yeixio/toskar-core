//go:build !windows

package terminal

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// stopWithChildren has stopping a command stop what it started too. A build
// runs many processes, and killing only the shell would leave them working.
func stopWithChildren(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		group := -cmd.Process.Pid
		if err := syscall.Kill(group, syscall.SIGTERM); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				return os.ErrProcessDone
			}
			return err
		}
		time.AfterFunc(stopGrace, func() { _ = syscall.Kill(group, syscall.SIGKILL) })
		return nil
	}
}
