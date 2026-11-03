package main

import (
	"syscall"
	"time"
)

// keepAwake stops the computer sleeping while this program runs. A long run is
// usually left alone. ES_SYSTEM_REQUIRED resets the idle timer each time it's said.
func keepAwake() func() {
	const esSystemRequired = 0x00000001
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("SetThreadExecutionState")
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			_, _, _ = proc.Call(esSystemRequired)
			select {
			case <-ticker.C:
			case <-done:
				return
			}
		}
	}()
	return func() { close(done) }
}
