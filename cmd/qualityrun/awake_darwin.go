package main

import (
	"os"
	"os/exec"
	"strconv"
)

// keepAwake stops the computer sleeping while this program runs. A long run is
// usually left alone, and a sleeping computer takes the model with it.
func keepAwake() func() {
	cmd := exec.Command("caffeinate", "-i", "-w", strconv.Itoa(os.Getpid()))
	if cmd.Start() != nil {
		return func() {}
	}
	return func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }
}
