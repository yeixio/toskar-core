package procmem

import (
	"strconv"
	"strings"
)

// allocated reads RssAnon and RssShmem from a /proc/<pid>/status, in bytes:
// what a process allocated, without the files it maps.
func allocated(status string) (uint64, bool) {
	var total uint64
	found := false
	for _, line := range strings.Split(status, "\n") {
		name, value, ok := strings.Cut(line, ":")
		if !ok || (name != "RssAnon" && name != "RssShmem") {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) == 0 {
			continue
		}
		kb, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		total += kb * 1024
		found = true
	}
	return total, found
}
