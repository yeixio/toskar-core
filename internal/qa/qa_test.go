package qa

import (
	"strings"
	"testing"
)

func TestRecorder(t *testing.T) {
	r := &recorder{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Errorf("first %d", 1)
		r.Fatalf("second")
		r.Errorf("never reached")
	}()
	<-done
	if !r.Failed() || strings.Join(r.messages(), "|") != "first 1|second" {
		t.Fatalf("recorded %v", r.messages())
	}
	clean := &recorder{TB: t}
	if clean.Failed() {
		t.Fatal("a fresh recorder has failed")
	}
}

// A known bug that's still there skips.
func TestKnownBugStillOpen(t *testing.T) {
	KnownBug(t, "toskar-core#0", func(t testing.TB) { t.Fatal("still broken") })
	t.Fatal("KnownBug returned instead of skipping")
}
