//go:build !linux && !windows

package procmem

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

// Resident is the memory process pid holds in RAM, in bytes, as ps reports
// its resident set.
func Resident(pid int) (uint64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, err
	}
	n, ok := parseKB(string(out))
	if !ok {
		return 0, fmt.Errorf("ps said %q for pid %d", out, pid)
	}
	return n, nil
}
