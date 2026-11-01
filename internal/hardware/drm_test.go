package hardware

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A fake /sys with the box the self-hosted runner is: a Radeon RX 7900 XTX
// and the processor's own graphics.
func fakeSys(t *testing.T) (sys, dev string) {
	t.Helper()
	// sysfs names have a colon (pci0000:00), which Windows doesn't allow
	// in a path, and the DRM reading is for Linux (found by
	// @black-operative in #382).
	if runtime.GOOS == "windows" {
		t.Skip("sysfs paths can't be made on Windows")
	}
	root := t.TempDir()
	dev = t.TempDir()
	card := func(name, slot, vendor, device, vram, render string) {
		pci := filepath.Join(root, "devices", "pci0000:00", slot)
		if err := os.MkdirAll(pci, 0o755); err != nil {
			t.Fatal(err)
		}
		for file, value := range map[string]string{"vendor": vendor, "device": device, "mem_info_vram_total": vram} {
			if value == "" {
				continue
			}
			if err := os.WriteFile(filepath.Join(pci, file), []byte(value+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.MkdirAll(filepath.Join(pci, "drm", render), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dev, render), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(root, "class", "drm", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(pci, filepath.Join(dir, "device")); err != nil {
			t.Fatal(err)
		}
	}
	card("card1", "0000:15:00.0", "0x1002", "0x13c0", "536870912", "renderD129")
	card("card0", "0000:03:00.0", "0x1002", "0x744c", "25753026560", "renderD128")
	// A connector, not a card.
	if err := os.MkdirAll(filepath.Join(root, "class", "drm", "card0-DP-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root, dev
}

func TestDRMGPUs(t *testing.T) {
	sys, dev := fakeSys(t)
	got := drmGPUs(sys, dev)
	if len(got) != 2 {
		t.Fatalf("got %d cards: %+v", len(got), got)
	}
	if g := got[0]; g.Slot != "0000:03:00.0" || g.Vendor != "1002" || g.Device != "744c" || g.VRAM != 25753026560 {
		t.Errorf("first card: %+v", g)
	}
	if g := got[1]; g.Slot != "0000:15:00.0" || g.VRAM != 536870912 {
		t.Errorf("second card: %+v", g)
	}
	if shortSlot(got[0].Slot) != "03:00.0" {
		t.Errorf("short slot %q", shortSlot(got[0].Slot))
	}
	if drmGPUs(t.TempDir(), dev) != nil {
		t.Error("cards found in an empty /sys")
	}
	// A container without --device /dev/dri sees /sys but no render nodes.
	if got := drmGPUs(sys, t.TempDir()); got != nil {
		t.Errorf("cards counted without their render nodes: %+v", got)
	}
}

func TestDRMAccelerators(t *testing.T) {
	sys, dev := fakeSys(t)
	cards := drmGPUs(sys, dev)
	names := map[string]string{"03:00.0": lspciName("03:00.0 VGA compatible controller: Advanced Micro Devices, Inc. [AMD/ATI] Navi 31 [Radeon RX 7900 XT/7900 XTX] (rev c8)")}
	got := drmAccelerators(cards, names, false, 0)
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Model != "AMD Radeon RX 7900 XT/7900 XTX" || got[0].DedicatedVRAM != 25753026560 || got[0].Backends[0] != "vulkan" {
		t.Errorf("the card lspci names: %+v", got[0])
	}
	if got[1].Model != "AMD GPU (1002:13c0)" || got[1].DedicatedVRAM != 536870912 {
		t.Errorf("the card without a name: %+v", got[1])
	}
	if rocm := drmAccelerators(cards, nil, true, 0); rocm[0].Backends[0] != "rocm" {
		t.Errorf("rocm: %v", rocm[0].Backends)
	}
	intel := []drmGPU{{Slot: "0000:00:02.0", Vendor: "8086", Device: "a780"}}
	if got := drmAccelerators(intel, nil, false, 0); len(got) != 1 || got[0].Vendor != "Intel" {
		t.Errorf("Intel alone: %+v", got)
	}
	if got := drmAccelerators(intel, nil, false, 1); len(got) != 0 {
		t.Errorf("Intel beside an NVIDIA card: %+v", got)
	}
}

// A card is named as people know it, not by its lspci line (#955).
func TestLSPCIName(t *testing.T) {
	for line, want := range map[string]string{
		"03:00.0 VGA compatible controller: Advanced Micro Devices, Inc. [AMD/ATI] Navi 31 [Radeon RX 7900 XT/7900 XTX/7900 GRE/7900M] (rev c8)": "AMD Radeon RX 7900 XT/7900 XTX/7900 GRE/7900M",
		"00:02.0 VGA compatible controller: Intel Corporation Raptor Lake-S GT1 [UHD Graphics 770] (rev 04)":                                     "Intel UHD Graphics 770",
		"00:02.0 VGA compatible controller: Intel Corporation Alder Lake-P GT2 (rev 0c)":                                                         "Intel Corporation Alder Lake-P GT2",
		"01:00.0 VGA compatible controller: NVIDIA Corporation AD104 [GeForce RTX 4070] (rev a1)":                                                "NVIDIA GeForce RTX 4070",
		"c1:00.0 Display controller: Advanced Micro Devices, Inc. [AMD/ATI] Strix Halo [Radeon 8060S]":                                           "AMD Radeon 8060S",
		"05:00.0 3D controller: Some Vendor Accelerator X":                                                                                       "Some Vendor Accelerator X",
	} {
		if got := lspciName(line); got != want {
			t.Errorf("lspciName(%q) = %q, want %q", line, got, want)
		}
	}
}
