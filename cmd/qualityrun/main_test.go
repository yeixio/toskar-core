package main

import (
	"bufio"
	"strings"
	"testing"
)

func TestHardwareLabelNamesNoComputer(t *testing.T) {
	type adapter = struct {
		Name          string `json:"name"`
		Model         string `json:"model"`
		DedicatedVRAM uint64 `json:"dedicated_vram_bytes"`
	}
	const gb = 1 << 30
	for _, c := range []struct {
		want     string
		adapters []adapter
	}{
		{"Some CPU, 64 GB", nil},
		{"Apple M5 Pro, 64 GB", []adapter{{Model: "Apple M5 Pro"}}},
		// The same name as the weekly run's reports give this card.
		{"AMD RX 7900 XTX, 24 GB", []adapter{{Model: "AMD Radeon RX 7900 XT/7900 XTX/7900 GRE/7900M", DedicatedVRAM: 24 * gb}}},
		{"Big Card, 16 GB", []adapter{{Model: "Built-in Graphics", DedicatedVRAM: gb}, {Model: "Big Card", DedicatedVRAM: 16 * gb}}},
		{"Some CPU, 64 GB", []adapter{{Model: "Microsoft Hyper-V Video"}}},
	} {
		var hw hardware
		hw.CPU.Model = "Some CPU"
		hw.Memory.TotalBytes = 64 * gb
		hw.Accelerators = c.adapters
		if got := hw.label(); got != c.want {
			t.Errorf("%q, want %q", got, c.want)
		}
	}
}

func TestAskerWithNobodyThere(t *testing.T) {
	if got := newAsker(true).ask("?"); got != "" {
		t.Errorf("--yes answered %q", got)
	}
	a := &asker{in: bufio.NewReader(strings.NewReader("  y \n"))}
	if got := a.ask("?"); got != "y" {
		t.Errorf("%q", got)
	}
	if got := a.ask("?"); got != "" {
		t.Errorf("after the input ended: %q", got)
	}
}
