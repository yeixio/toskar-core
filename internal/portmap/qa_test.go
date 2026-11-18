package portmap

import (
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/qa"
)

// A crash leaves the router's mapping behind only for a short lease, not
// two hours.
func TestQAShortLease(t *testing.T) {
	// Regression: toskar-core#1262
	qa.KnownBug(t, "toskar-core#1262", func(t testing.TB) {
		if Lifetime > 20*time.Minute {
			t.Errorf("mappings are asked for %v; after a crash the port stays open that long", Lifetime)
		}
	})
}
