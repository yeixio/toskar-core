// Package procmem measures how much memory a process holds, for the memory a
// running model really uses beside the estimate (#1066).
package procmem

import (
	"strconv"
	"strings"
)

// parseKB reads a figure in KB, such as ps's "123456" or tasklist's
// "1,234,567 K", as bytes.
func parseKB(s string) (uint64, bool) {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "K"))
	s = strings.NewReplacer(",", "", ".", "", " ", "", " ", "").Replace(s)
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil || n == 0 {
		return 0, false
	}
	return n * 1024, true
}
