package procmem

import (
	"context"
	"encoding/csv"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Resident is the memory process pid holds in RAM, in bytes: its working
// set, as tasklist reports it.
func Resident(pid int) (uint64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH").Output()
	if err != nil {
		return 0, err
	}
	// "llama-server.exe","1234","Console","1","1,234,567 K"
	row, err := csv.NewReader(strings.NewReader(string(out))).Read()
	if err != nil || len(row) < 5 {
		return 0, fmt.Errorf("tasklist said %q for pid %d", out, pid)
	}
	n, ok := parseKB(row[4])
	if !ok {
		return 0, fmt.Errorf("tasklist said %q for pid %d", row[4], pid)
	}
	return n, nil
}
