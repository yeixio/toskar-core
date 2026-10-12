package relayclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"
)

func TestCheck(t *testing.T) {
	f := newFakeRelay(t)
	c, cert, _ := f.client(t, f.secret, nil)
	ctx := context.Background()
	got, err := c.Check(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The relay gets this computer's certificate and its direct ways in.
	sum := sha256.Sum256(cert.Certificate[0])
	if f.checked["fingerprint"] != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Fatalf("fingerprint sent: %v", f.checked["fingerprint"])
	}
	if a, _ := f.checked["addresses"].([]any); len(a) != 1 || a[0] != "203.0.113.7:7333" {
		t.Fatalf("addresses sent: %v", f.checked["addresses"])
	}
	if !got.Relay.OK || len(got.Direct) != 1 || got.Direct[0].Reason != "timeout" || time.Since(got.At) > time.Minute {
		t.Fatalf("result: %+v", got)
	}
	// The relay's once a minute, and a relay from before the check.
	if _, err := c.Check(ctx); !errors.Is(err, ErrCheckTooSoon) {
		t.Fatalf("too soon: %v", err)
	}
	f.mu.Lock()
	f.noCheck = true
	f.mu.Unlock()
	if _, err := c.Check(ctx); !errors.Is(err, ErrCheckUnsupported) {
		t.Fatalf("older relay: %v", err)
	}
}
