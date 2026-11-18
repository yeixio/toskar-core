package app

import (
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/auth"
)

// A device key never used a day after pairing, or unused for 90 days, is
// counted as likely left over (#1266).
func TestStaleDeviceKey(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-2 * time.Hour)
	old := now.Add(-100 * 24 * time.Hour)
	cases := []struct {
		name  string
		key   auth.APIKeyRecord
		stale bool
	}{
		{"just paired", auth.APIKeyRecord{CreatedAt: recent}, false},
		{"never used", auth.APIKeyRecord{CreatedAt: now.Add(-48 * time.Hour)}, true},
		{"used lately", auth.APIKeyRecord{CreatedAt: old, LastUsed: &recent}, false},
		{"unused for months", auth.APIKeyRecord{CreatedAt: old, LastUsed: &old}, true},
	}
	for _, c := range cases {
		if got := staleDeviceKey(c.key, now); got != c.stale {
			t.Errorf("%s: stale = %v", c.name, got)
		}
	}
}
