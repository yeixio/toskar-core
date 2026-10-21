package license

import (
	"errors"
	"testing"
	"time"
)

func TestNothingIsLimitedWithoutPro(t *testing.T) {
	k := Open(t.TempDir())
	if got := k.Get(); got.Kind != None || got.Limited() {
		t.Fatalf("a Core on its own = %+v, want None and not limited", got)
	}
	var none *Keeper
	if none.Get().Limited() {
		t.Fatal("no keeper limits nothing")
	}
}

func TestATrialEndsOnItsOwn(t *testing.T) {
	dir := t.TempDir()
	k := Open(dir)
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	k.now = func() time.Time { return now }
	ends := now.Add(14 * 24 * time.Hour)
	if _, err := k.Set(State{Kind: Trial, TrialEnds: ends, Store: "mac"}); err != nil {
		t.Fatal(err)
	}
	if got := k.Get(); got.Kind != Trial || got.Limited() {
		t.Fatalf("during the trial = %+v", got)
	}
	now = ends
	if got := k.Get(); got.Kind != Ended || !got.Limited() {
		t.Fatalf("at its end = %+v, want Ended", got)
	}

	// Kept across a restart, and still ended.
	again := Open(dir)
	again.now = func() time.Time { return now }
	if got := again.Get(); got.Kind != Ended || got.Store != "mac" || !got.TrialEnds.Equal(ends) {
		t.Fatalf("after a restart = %+v", got)
	}
}

func TestUnlockedDropsTheTrialEnd(t *testing.T) {
	k := Open(t.TempDir())
	got, err := k.Set(State{Kind: Unlocked, TrialEnds: time.Now().Add(-time.Hour), Store: "windows"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != Unlocked || !got.TrialEnds.IsZero() || got.Limited() {
		t.Fatalf("unlocked = %+v", got)
	}
}

func TestRefusesWhatProMustNotSend(t *testing.T) {
	k := Open(t.TempDir())
	for _, s := range []State{{Kind: "lifetime"}, {Kind: Trial}} {
		if _, err := k.Set(s); !errors.Is(err, ErrInvalid) {
			t.Errorf("Set(%+v) = %v, want ErrInvalid", s, err)
		}
	}
	if k.Get().Kind != None {
		t.Fatal("a refused state was kept")
	}
}
