package app

import "testing"

// Apple silicon's graphics may use about two thirds of its memory, and
// three quarters above 36 GB: 24 GB of memory isn't 24 GB of graphics
// memory (#550).
func TestSharedGraphics(t *testing.T) {
	for _, c := range []struct{ memory, want uint64 }{
		{24 << 30, 16 << 30},
		{36 << 30, 24 << 30},
		{48 << 30, 36 << 30},
		{128 << 30, 96 << 30},
	} {
		if got := sharedGraphics(c.memory); got != c.want {
			t.Errorf("%d GB: %d GB, want %d GB", c.memory>>30, got>>30, c.want>>30)
		}
	}
}
