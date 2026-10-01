package llamacpp

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWaitReadyStopsWhenTheProcessExits(t *testing.T) {
	exited := make(chan struct{})
	go func() {
		time.Sleep(100 * time.Millisecond)
		close(exited)
	}()
	start := time.Now()
	// Nothing listens on this port, so only the exit can end the wait early.
	err := waitReady(context.Background(), "http://127.0.0.1:1", 30*time.Second, exited)
	if !errors.Is(err, errExitedWhileLoading) {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("waited %s after the process exited", time.Since(start))
	}
}
