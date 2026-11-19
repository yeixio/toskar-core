//go:build windows

package connectors

import (
	"errors"
	"os"
)

var errNoFIFO = errors.New("hearing a camera isn't available on Windows yet")

func makeFIFO(string) error                   { return errNoFIFO }
func openFIFOWriter(string) (*os.File, error) { return nil, errNoFIFO }
