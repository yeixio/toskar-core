package hardware

import (
	"context"
	"runtime"
	"testing"
	"time"
)

func TestDetectReturnsBasics(t *testing.T) {
	d := &Detector{DataPath: t.TempDir()}
	inv, err := d.Detect(context.Background())
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if inv.OS != runtime.GOOS {
		t.Fatalf("os: got %s want %s", inv.OS, runtime.GOOS)
	}
	if inv.Arch != runtime.GOARCH {
		t.Fatalf("arch: got %s want %s", inv.Arch, runtime.GOARCH)
	}
	if inv.CPU.Cores <= 0 {
		t.Fatalf("expected CPU cores > 0")
	}
	if inv.Memory.TotalBytes == 0 {
		t.Fatalf("expected memory total > 0")
	}
	if inv.Disk.AvailableBytes == 0 && inv.Disk.TotalBytes == 0 {
		t.Log("disk totals unavailable; tolerated")
	}
	if len(inv.Accelerators) == 0 {
		t.Fatalf("expected at least one accelerator entry")
	}
}

// One turn asks for the inventory several times; within MaxAge it's looked
// up once, and each caller gets its own copy (#795).
func TestDetectKeepsInventoryForMaxAge(t *testing.T) {
	d := &Detector{DataPath: t.TempDir(), MaxAge: time.Minute}
	first, err := d.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := d.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !second.DetectedAt.Equal(first.DetectedAt) {
		t.Fatal("looked again within MaxAge")
	}
	if len(first.Accelerators) > 0 {
		first.Accelerators[0].Model = "changed"
		third, _ := d.Detect(context.Background())
		if third.Accelerators[0].Model == "changed" {
			t.Fatal("a caller's change reached the kept inventory")
		}
	}
	d.mu.Lock()
	d.lastAt = time.Now().Add(-2 * time.Minute)
	d.mu.Unlock()
	fresh, err := d.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !fresh.DetectedAt.After(first.DetectedAt) {
		t.Fatal("didn't look again after MaxAge")
	}
}

// After a pause longer than MaxAge but within StaleAge, the kept inventory
// answers at once and a fresh look runs behind it (#795).
func TestDetectRefreshesInBackground(t *testing.T) {
	d := &Detector{DataPath: t.TempDir(), MaxAge: time.Minute, StaleAge: time.Hour}
	first, err := d.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	d.lastAt = time.Now().Add(-2 * time.Minute)
	d.mu.Unlock()
	stale, err := d.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !stale.DetectedAt.Equal(first.DetectedAt) {
		t.Fatal("waited for a fresh look within StaleAge")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		d.mu.Lock()
		done := !d.refreshing && d.last.DetectedAt.After(first.DetectedAt)
		d.mu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no fresh look in the background")
		}
		time.Sleep(10 * time.Millisecond)
	}
	d.mu.Lock()
	d.lastAt = time.Now().Add(-2 * time.Hour)
	d.mu.Unlock()
	fresh, err := d.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.refreshing || !fresh.DetectedAt.Equal(d.last.DetectedAt) {
		t.Fatal("past StaleAge, didn't look before answering")
	}
}
