package main

import "os/exec"

// keepAwake stops the computer sleeping while this program runs, where systemd
// can say so. A long run is usually left alone.
func keepAwake() func() {
	cmd := exec.Command("systemd-inhibit", "--what=sleep:idle", "--who=Toskar", "--why=Measuring a model", "sleep", "infinity")
	if cmd.Start() != nil {
		return func() {}
	}
	return func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }
}
