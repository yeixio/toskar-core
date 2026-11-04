package terminal

import (
	"os/exec"
	"strconv"
)

// stopWithChildren has stopping a command stop what it started too. A build
// runs many processes, and killing only the shell would leave them working.
func stopWithChildren(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if err := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run(); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}
