package procmem

import (
	"os"
	"testing"
)

// This process holds some memory, as each system reports it.
func TestResidentOfThisProcess(t *testing.T) {
	n, err := Resident(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if n < 1<<20 || n > 1<<40 {
		t.Errorf("this process holds %d bytes", n)
	}
	if _, err := Resident(1 << 30); err == nil {
		t.Error("a process that doesn't exist was measured")
	}
}

func TestParseKB(t *testing.T) {
	for in, want := range map[string]uint64{"123456\n": 123456 * 1024, " 1,234,567 K": 1234567 * 1024, "1.234.567 K": 1234567 * 1024, "": 0, "n/a": 0} {
		got, ok := parseKB(in)
		if got != want || ok != (want > 0) {
			t.Errorf("parseKB(%q) = %d, %v", in, got, ok)
		}
	}
}
