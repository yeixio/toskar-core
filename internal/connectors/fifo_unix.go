//go:build !windows

package connectors

import (
	"os"
	"syscall"
)

func makeFIFO(path string) error { return syscall.Mkfifo(path, 0o600) }

// openFIFOWriter opens the pipe for writing without waiting for a reader;
// it fails when there's none.
func openFIFOWriter(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0)
}
