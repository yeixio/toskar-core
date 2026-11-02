package main

import (
	"bufio"
	"strings"
	"testing"
)

func TestHardwareLabelNamesNoComputer(t *testing.T) {
	var hw hardware
	hw.CPU.Model = "Some CPU"
	hw.Memory.TotalBytes = 25769803776
	if got := hw.label(); got != "Some CPU, 24 GB" {
		t.Error(got)
	}
	hw.Accelerators = append(hw.Accelerators, struct {
		Name          string `json:"name"`
		Model         string `json:"model"`
		DedicatedVRAM uint64 `json:"dedicated_vram_bytes"`
	}{Model: "Some Card"})
	if got := hw.label(); got != "Some Card, 24 GB" {
		t.Error(got)
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
