package llamacpp

import (
	"os"
	"testing"
	"time"
)

// A running model's memory is measured from its process (#1066), and the
// reading stands for residentFor: the running list is asked for often.
func TestResidentOf(t *testing.T) {
	s := NewSupervisor(t.TempDir())
	if got := s.residentOf(0); got != 0 {
		t.Errorf("no process: %d", got)
	}
	pid := os.Getpid()
	first := s.residentOf(pid)
	if first < 1<<20 {
		t.Fatalf("this process holds %d bytes", first)
	}
	// A reading within residentFor is the one kept, not measured again.
	s.residentMu.Lock()
	s.resident[pid] = residentReading{bytes: 42, at: time.Now()}
	s.residentMu.Unlock()
	if got := s.residentOf(pid); got != 42 {
		t.Errorf("a fresh reading was measured again: %d", got)
	}
	// An older one is measured again.
	s.residentMu.Lock()
	s.resident[pid] = residentReading{bytes: 42, at: time.Now().Add(-residentFor - time.Second)}
	s.residentMu.Unlock()
	if got := s.residentOf(pid); got == 42 || got < 1<<20 {
		t.Errorf("an old reading wasn't measured again: %d", got)
	}
}
