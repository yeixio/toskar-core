package models

import (
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A mixture-of-experts model's cache and speed follow its active
// parameters, not all of them (#1140): treated as dense, gpt-oss-20b got an
// 8K window on a 32 GB Mac and a third of its speed.
func TestMixtureOfExpertsFit(t *testing.T) {
	c, err := NewCatalogEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	moe, ok := c.Get("gpt-oss-20b-mxfp4")
	if !ok {
		t.Fatal("gpt-oss-20b-mxfp4 isn't in the catalog")
	}
	dense := moe
	dense.ActiveParameters = ""
	mac := apple(gib(32), gib(22))
	m, d := ClassifyFit(moe, mac, "", "", FitOptions{}), ClassifyFit(dense, mac, "", "", FitOptions{})
	if m.ContextTokens <= d.ContextTokens {
		t.Errorf("window %d, not more than dense's %d", m.ContextTokens, d.ContextTokens)
	}
	if m.EstTokPerSec <= 2*d.EstTokPerSec {
		t.Errorf("%.1f tokens a second, not more than twice dense's %.1f", m.EstTokPerSec, d.EstTokPerSec)
	}
	same := ClassifyFit(dense, mac, "", "", FitOptions{ContextTokens: m.ContextTokens})
	if m.RuntimeMemoryLowBytes >= same.RuntimeMemoryLowBytes {
		t.Errorf("needs %d bytes at a %d window, not less than dense's %d", m.RuntimeMemoryLowBytes, m.ContextTokens, same.RuntimeMemoryLowBytes)
	}

	// Active parameters that aren't fewer than all of them are a dense model.
	odd := dense
	odd.ActiveParameters = "21B"
	if got := ClassifyFit(odd, mac, "", "", FitOptions{}); got.EstTokPerSec != d.EstTokPerSec || got.RuntimeMemoryLowBytes != d.RuntimeMemoryLowBytes {
		t.Errorf("active = all: %+v, want dense's %+v", got, d)
	}
}

// gpt-oss fits where its files say it does: the 20B from 24 GB, the 120B
// only on a computer with far more (#1140).
func TestGptOssFits(t *testing.T) {
	c, err := NewCatalogEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id   string
		gb   float64
		want []contracts.FitLabel
	}{
		{"gpt-oss-20b-mxfp4", 16, []contracts.FitLabel{contracts.FitHeavy, contracts.FitTight}},
		{"gpt-oss-20b-mxfp4", 24, []contracts.FitLabel{contracts.FitGood, contracts.FitExcellent}},
		{"gpt-oss-120b-mxfp4", 32, []contracts.FitLabel{contracts.FitUnsupported}},
		{"gpt-oss-120b-mxfp4", 64, []contracts.FitLabel{contracts.FitHeavy, contracts.FitTight}},
		{"gpt-oss-120b-mxfp4", 128, []contracts.FitLabel{contracts.FitGood, contracts.FitExcellent}},
	} {
		e, ok := c.Get(tc.id)
		if !ok {
			t.Fatalf("%s isn't in the catalog", tc.id)
		}
		fit := ClassifyFit(e, apple(gib(tc.gb), gib(tc.gb*0.7)), "", "", FitOptions{})
		found := false
		for _, l := range tc.want {
			found = found || fit.Label == l
		}
		if !found {
			t.Errorf("%s on %.0f GB: %s, want one of %v", tc.id, tc.gb, fit.Label, tc.want)
		}
		if !e.Capabilities.Thinks || !e.Capabilities.ToolCalling {
			t.Errorf("%s: thinks %v, tools %v", tc.id, e.Capabilities.Thinks, e.Capabilities.ToolCalling)
		}
	}
}

// A mixture-of-experts model bigger than the graphics card runs with its
// experts in system memory and the rest on the card (#984), so it isn't held
// to the card as a dense model of its size is. A card too small for even
// what each token reads holds it as before.
func TestMixtureOfExpertsOnACard(t *testing.T) {
	c, err := NewCatalogEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	moe, ok := c.Get("qwen3.6-35b-a3b-q4")
	if !ok {
		t.Fatal("qwen3.6-35b-a3b-q4 isn't in the catalog")
	}
	dense := moe
	dense.ActiveParameters = ""
	card := func(vram float64) contracts.HardwareInventory {
		return contracts.HardwareInventory{
			Memory:       contracts.MemoryInfo{TotalBytes: gib(32), AvailableBytes: gib(26)},
			Accelerators: []contracts.Accelerator{{Kind: "gpu", Vendor: "NVIDIA", DedicatedVRAM: gib(vram), Backends: []string{"cuda", "cpu"}}},
		}
	}
	fit := ClassifyFit(moe, card(16), "n", "Laptop", FitOptions{})
	if fit.Label != contracts.FitGood || fit.Short != "" || !strings.Contains(fit.Reason, "experts run from system memory") {
		t.Errorf("on a 16 GB card: %s short=%q %q", fit.Label, fit.Short, fit.Reason)
	}
	if d := ClassifyFit(dense, card(16), "n", "Laptop", FitOptions{}); fitRank(d.Label) > fitRank(contracts.FitTight) || d.Short != "graphics" {
		t.Errorf("dense on a 16 GB card: %s short=%q", d.Label, d.Short)
	}
	if small := ClassifyFit(moe, card(2), "n", "PC", FitOptions{}); fitRank(small.Label) > fitRank(contracts.FitTight) || small.Short != "graphics" {
		t.Errorf("on a 2 GB card: %s short=%q", small.Label, small.Short)
	}
	// On a card it fits, it's as any model is.
	if big := ClassifyFit(moe, card(24), "n", "Rig", FitOptions{}); fitRank(big.Label) < fitRank(contracts.FitGood) || big.Short != "" || strings.Contains(big.Reason, "experts") {
		t.Errorf("on a 24 GB card: %s short=%q", big.Label, big.Short)
	}
}

// A model split between the graphics card and system memory gets no speed
// until it's measured here (#984): the estimate said about 62 tokens a
// second for Qwen 3.6 35B-A3B on a 16 GB laptop card, which answered at
// 6.3, and 19 for Qwen 2.5 32B, which answered at 3.8. One that fits the
// card keeps its estimate, and a measured run fills one in.
func TestSplitModelsHaveNoGuessedSpeed(t *testing.T) {
	c, err := NewCatalogEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	laptop := func(vram float64) contracts.HardwareInventory {
		return contracts.HardwareInventory{
			Memory:       contracts.MemoryInfo{TotalBytes: gib(32), AvailableBytes: gib(26)},
			Accelerators: []contracts.Accelerator{{Kind: "gpu", Vendor: "NVIDIA", DedicatedVRAM: gib(vram), Backends: []string{"vulkan", "cpu"}}},
		}
	}
	for _, id := range []string{"qwen3.6-35b-a3b-q4", "qwen2.5-32b-q4"} {
		e, _ := c.Get(id)
		if fit := ClassifyFit(e, laptop(16), "n", "Laptop", FitOptions{}); fit.EstTokPerSec != 0 {
			t.Errorf("%s on a 16 GB card: %.1f tokens a second, want none", id, fit.EstTokPerSec)
		}
		if fit := ClassifyFit(e, laptop(24), "n", "Rig", FitOptions{}); fit.EstTokPerSec <= 0 {
			t.Errorf("%s on a 24 GB card: no estimate", id)
		}
		measured := FitOptions{Measured: map[string]MeasuredRun{id: {TokPerSec: 6.3}}}
		if fit := ClassifyFit(e, laptop(16), "n", "Laptop", measured); fit.EstTokPerSec != 6.3 || !fit.TokPerSecMeasured {
			t.Errorf("%s measured on a 16 GB card: %.1f, measured %v", id, fit.EstTokPerSec, fit.TokPerSecMeasured)
		}
	}
	// Without a card, on the processor alone, the estimate stays as it was.
	e, _ := c.Get("qwen2.5-7b-q4")
	if fit := ClassifyFit(e, contracts.HardwareInventory{Memory: contracts.MemoryInfo{TotalBytes: gib(32), AvailableBytes: gib(26)}}, "n", "PC", FitOptions{}); fit.EstTokPerSec <= 0 {
		t.Errorf("7B without a card: no estimate")
	}
}
