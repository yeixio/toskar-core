package procmem

import (
	"fmt"
	"os"
)

// Resident is the memory process pid allocated and holds in RAM, in bytes:
// its anonymous and shared pages, from /proc/<pid>/status. Pages of files it
// maps aren't counted: a model's file stays mapped after its weights went to
// the GPU, and how much of it is resident is the file cache's doing, so the
// same model measured 9.3 GB, then 2.8 GB, on one computer (#1066).
func Resident(pid int) (uint64, error) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, err
	}
	n, ok := allocated(string(raw))
	if !ok {
		return 0, fmt.Errorf("unreadable /proc/%d/status", pid)
	}
	return n, nil
}
