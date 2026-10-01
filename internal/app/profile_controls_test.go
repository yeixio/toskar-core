package app

import (
	"strings"
	"testing"
)

func TestStoppedAtTheProfilesTimeLimit(t *testing.T) {
	tr := &turnTrace{}
	tr.stopped(false, true)
	m := tr.meta()
	if m == nil || !strings.Contains(m.Notice, "time limit") || m.Steps[0].Text != "Stopped at this profile's time limit" {
		t.Fatalf("meta = %+v", m)
	}
	tr = &turnTrace{}
	tr.stopped(true, false)
	if m := tr.meta(); m.Notice != "Stopped before the answer was finished." || m.Steps[0].Text != "Stopped by you" {
		t.Fatalf("meta = %+v", m)
	}
}
