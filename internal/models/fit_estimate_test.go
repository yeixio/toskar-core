package models

import (
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

func gib(n float64) uint64 {
	return uint64(n * 1024 * 1024 * 1024)
}

func apple(total, available uint64) contracts.HardwareInventory {
	return contracts.HardwareInventory{
		OS:   "darwin",
		Arch: "arm64",
		Memory: contracts.MemoryInfo{
			TotalBytes:     total,
			AvailableBytes: available,
			SwapTotalBytes: gib(8),
		},
		Accelerators: []contracts.Accelerator{{
			Vendor:        "Apple",
			Model:         "Apple M-series",
			Kind:          "gpu",
			UnifiedMemory: total,
			Backends:      []string{"metal", "cpu"},
		}},
	}
}

func qwen32(size uint64) CatalogEntry {
	return CatalogEntry{
		ID:          "qwen2.5-32b-q4",
		DisplayName: "Qwen 2.5 32B",
		Variant:     "Q4_K_M",
		Parameters:  "32B",
		SizeBytes:   size,
		Context:     32768,
		Runtime:     []string{"llamacpp"},
	}
}

func TestApple24GBQwen32IsNotUnsupported(t *testing.T) {
	fit := ClassifyFit(qwen32(gib(22.4)), apple(gib(24), gib(17)), "local", "This Mac", FitOptions{})
	if fit.Label == contracts.FitUnsupported || fit.Label == contracts.FitTooLarge {
		t.Fatalf("label %s headroom %.2f low %d", fit.Label, fit.Headroom, fit.RuntimeMemoryLowBytes)
	}
	if fit.Label != contracts.FitTight && fit.Label != contracts.FitHeavy {
		t.Fatalf("expected tight or heavy, got %s (headroom %.2f)", fit.Label, fit.Headroom)
	}
	if !fit.InstallAllowed {
		t.Fatal("install should stay available")
	}
	if fit.ContextTokens != runningContextTokens {
		t.Fatalf("context %d, want running window %d", fit.ContextTokens, runningContextTokens)
	}
	if fit.MemoryKind != "unified" {
		t.Fatalf("memory kind %q", fit.MemoryKind)
	}
}

func TestApple16GBQwen32IsHeavyOrUnsupported(t *testing.T) {
	fit := ClassifyFit(qwen32(gib(22.4)), apple(gib(16), gib(10)), "local", "This Mac", FitOptions{})
	if fit.Label != contracts.FitHeavy && fit.Label != contracts.FitUnsupported {
		t.Fatalf("expected heavy or unsupported, got %s", fit.Label)
	}
	if fit.Label == contracts.FitExcellent || fit.Label == contracts.FitGood || fit.Label == contracts.FitTight {
		t.Fatal("16 GB should not look comfortable")
	}
}

func TestApple48GBQwen32IsComfortable(t *testing.T) {
	fit := ClassifyFit(qwen32(gib(22.4)), apple(gib(48), gib(40)), "local", "Studio", FitOptions{})
	if fit.Label != contracts.FitGood && fit.Label != contracts.FitExcellent {
		t.Fatalf("expected good or excellent, got %s (headroom %.2f)", fit.Label, fit.Headroom)
	}
}

func TestLongerContextFitsWorse(t *testing.T) {
	hw := apple(gib(24), gib(18))
	model := qwen32(gib(22.4))
	short := ClassifyFit(model, hw, "local", "This Mac", FitOptions{ContextTokens: 8192})
	long := ClassifyFit(model, hw, "local", "This Mac", FitOptions{ContextTokens: 32768})
	if long.RuntimeMemoryLowBytes <= short.RuntimeMemoryLowBytes {
		t.Fatalf("32k low %d should exceed 8k low %d", long.RuntimeMemoryLowBytes, short.RuntimeMemoryLowBytes)
	}
	if fitRank(long.Label) > fitRank(short.Label) {
		t.Fatalf("32k %s should not outrank 8k %s", long.Label, short.Label)
	}
	if long.Label == short.Label && long.Headroom >= short.Headroom {
		t.Fatalf("32k headroom %.2f should be worse than 8k %.2f", long.Headroom, short.Headroom)
	}
}

func TestQ4FitsBetterThanFP16(t *testing.T) {
	hw := apple(gib(48), gib(40))
	q4 := ClassifyFit(CatalogEntry{
		ID: "q4", DisplayName: "Q4", Parameters: "32B", Variant: "Q4_K_M", Runtime: []string{"llamacpp"}, Context: 8192,
	}, hw, "local", "This Mac", FitOptions{})
	fp16 := ClassifyFit(CatalogEntry{
		ID: "fp16", DisplayName: "FP16", Parameters: "32B", Variant: "FP16", Runtime: []string{"llamacpp"}, Context: 8192,
	}, hw, "local", "This Mac", FitOptions{})
	if q4.WeightBytes >= fp16.WeightBytes {
		t.Fatalf("q4 weights %d fp16 %d", q4.WeightBytes, fp16.WeightBytes)
	}
	if fitRank(q4.Label) <= fitRank(fp16.Label) {
		t.Fatalf("q4 %s should outrank fp16 %s", q4.Label, fp16.Label)
	}
	if !q4.Approximate || !fp16.Approximate {
		t.Fatal("estimates without a file size should be marked approximate")
	}
}

func TestLowFreeMemoryStillAllowsInstall(t *testing.T) {
	// Runtime sits around 19 GB. Total 24 GB can hold it. 11 GB free cannot, right now.
	fit := ClassifyFit(qwen32(gib(16)), apple(gib(24), gib(11)), "local", "This Mac", FitOptions{})
	if !fit.InstallAllowed || fit.Label == contracts.FitUnsupported {
		t.Fatalf("label %s allowed %v", fit.Label, fit.InstallAllowed)
	}
	if !strings.Contains(fit.RuntimeWarning, "Close other applications") {
		t.Fatalf("warning %q", fit.RuntimeWarning)
	}
}

func TestMeasuredRunOverridesGenericUnsupported(t *testing.T) {
	hw := apple(gib(24), gib(16))
	model := qwen32(gib(40))
	generic := ClassifyFit(model, hw, "local", "This Mac", FitOptions{})
	if generic.Label != contracts.FitUnsupported {
		t.Fatalf("generic label %s, want unsupported so the override is meaningful", generic.Label)
	}
	measured := ClassifyFit(model, hw, "local", "This Mac", FitOptions{
		Measured: map[string]MeasuredRun{
			model.ID: {TokPerSec: 26.4, PeakMemoryBytes: gib(21.8)},
		},
	})
	if measured.Label == contracts.FitUnsupported || !measured.InstallAllowed {
		t.Fatalf("measured label %s allowed %v", measured.Label, measured.InstallAllowed)
	}
	if !measured.TokPerSecMeasured || measured.EstTokPerSec != 26.4 {
		t.Fatalf("tok/s %+v", measured.EstTokPerSec)
	}
	if !strings.Contains(measured.Reason, "Measured peak memory") {
		t.Fatalf("reason %q", measured.Reason)
	}
}

func TestDiscreteGPUOffloadIsNotUnsupported(t *testing.T) {
	hw := contracts.HardwareInventory{
		Memory: contracts.MemoryInfo{TotalBytes: gib(64), AvailableBytes: gib(40)},
		Accelerators: []contracts.Accelerator{{
			Vendor:        "AMD",
			Model:         "Radeon 7900 XTX",
			Kind:          "gpu",
			DedicatedVRAM: gib(16),
			Backends:      []string{"hip", "cpu"},
		}},
	}
	fit := ClassifyFit(qwen32(gib(22)), hw, "desktop", "Desktop", FitOptions{})
	if fit.Label == contracts.FitUnsupported || fit.MemoryKind == "unified" {
		t.Fatalf("label %s kind %s", fit.Label, fit.MemoryKind)
	}
	if fit.GPUNote != "Partial GPU offload expected" {
		t.Fatalf("gpu note %q", fit.GPUNote)
	}
	if !fit.InstallAllowed {
		t.Fatal("system RAM can hold the model")
	}
}

func TestIncompatibleBackendIsUnsupported(t *testing.T) {
	model := qwen32(gib(4))
	model.Runtime = []string{"cuda"}
	fit := ClassifyFit(model, apple(gib(128), gib(100)), "local", "This Mac", FitOptions{})
	if fit.Label != contracts.FitUnsupported || fit.InstallAllowed {
		t.Fatalf("label %s allowed %v", fit.Label, fit.InstallAllowed)
	}
}

func TestProvenLoadIsNotDowngraded(t *testing.T) {
	model := qwen32(gib(40))
	fit := ClassifyFit(model, apple(gib(24), gib(12)), "local", "This Mac", FitOptions{
		Proven: map[string]bool{model.ID: true},
	})
	if fit.Label == contracts.FitUnsupported || fit.Label == contracts.FitTooLarge {
		t.Fatalf("label %s", fit.Label)
	}
	if !fit.InstallAllowed || !fit.KnownToRun {
		t.Fatalf("allowed %v known %v", fit.InstallAllowed, fit.KnownToRun)
	}
	if !strings.Contains(fit.Reason, "Known to run") {
		t.Fatalf("reason %q", fit.Reason)
	}
}

func TestLoadedModelWarnsWithoutChangingFit(t *testing.T) {
	hw := apple(gib(24), gib(8))
	alone := ClassifyFit(qwen32(gib(16)), hw, "local", "This Mac", FitOptions{})
	busy := ClassifyFit(qwen32(gib(16)), hw, "local", "This Mac", FitOptions{
		LoadedModelNames: []string{"Qwen 14B"},
	})
	if busy.Label != alone.Label {
		t.Fatalf("loaded models changed fit from %s to %s", alone.Label, busy.Label)
	}
	if !strings.Contains(busy.RuntimeWarning, "Qwen 14B") {
		t.Fatalf("warning %q", busy.RuntimeWarning)
	}
}

// On Apple silicon a model is held to what the graphics may use, with a
// model that sees pictures counting its projector (#751): Muse Glimmer 30B
// ran out of graphics memory with its projector on a 24 GB Mac, and Qwen
// 2.5 32B can't fit there at all.
func TestFitHoldsMacsToTheirGraphicsShare(t *testing.T) {
	catalog, err := NewCatalogEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	glimmer, ok := catalog.Get("muse-glimmer-30b-q4")
	if !ok {
		t.Fatal("muse-glimmer-30b-q4 not in the catalog")
	}
	qwen, _ := catalog.Get("qwen2.5-32b-q4")
	gemma, _ := catalog.Get("gemma-3-4b-q4")

	mac24, mac36 := apple(gib(24), gib(20)), apple(gib(36), gib(30))
	for _, c := range []struct {
		name  string
		entry CatalogEntry
		hw    contracts.HardwareInventory
		worst contracts.FitLabel // the best label it may get
		short bool
	}{
		{"Muse Glimmer on 24 GB", glimmer, mac24, contracts.FitHeavy, true},
		{"Qwen 2.5 32B on 24 GB", qwen, mac24, contracts.FitHeavy, true},
		{"Gemma 3 4B on 24 GB", gemma, mac24, contracts.FitExcellent, false},
	} {
		fit := ClassifyFit(c.entry, c.hw, "n", "Mac", FitOptions{})
		if fitRank(fit.Label) > fitRank(c.worst) || (c.short != (fit.Short == "shared")) {
			t.Errorf("%s: %s %q short=%q", c.name, fit.Label, fit.Reason, fit.Short)
		}
		if c.short && (fit.HaveBytes != SharedGraphics(gib(24)) || fit.NeedBytes <= fit.HaveBytes || !strings.Contains(fit.Reason, "graphics memory")) {
			t.Errorf("%s: need %d have %d, %q", c.name, fit.NeedBytes, fit.HaveBytes, fit.Reason)
		}
	}
	// With room for it, it's offered: good or better on 36 GB.
	if fit := ClassifyFit(glimmer, mac36, "n", "Mac", FitOptions{}); fitRank(fit.Label) < fitRank(contracts.FitGood) {
		t.Errorf("Muse Glimmer on 36 GB: %s %q", fit.Label, fit.Reason)
	}
	// The projector counts: the same model without one needs less.
	plain := glimmer
	plain.Projector = nil
	with, without := ClassifyFit(glimmer, mac36, "n", "Mac", FitOptions{}), ClassifyFit(plain, mac36, "n", "Mac", FitOptions{})
	if with.ExpectedMemoryBytes-without.ExpectedMemoryBytes != glimmer.Projector.SizeBytes+visionComputeBytes {
		t.Errorf("projector adds %d", with.ExpectedMemoryBytes-without.ExpectedMemoryBytes)
	}
	// A graphics card's own memory isn't shared: a 24 GB card is judged as before.
	card := contracts.HardwareInventory{
		Memory:       contracts.MemoryInfo{TotalBytes: gib(64), AvailableBytes: gib(48)},
		Accelerators: []contracts.Accelerator{{Kind: "gpu", DedicatedVRAM: gib(24), Backends: []string{"vulkan", "cpu"}}},
	}
	if fit := ClassifyFit(glimmer, card, "n", "PC", FitOptions{}); fit.Short != "" || fitRank(fit.Label) < fitRank(contracts.FitGood) {
		t.Errorf("Muse Glimmer with a 24 GB card: %s %q short=%q", fit.Label, fit.Reason, fit.Short)
	}
}

// A graphics card's own memory holds a model back as a Mac's graphics share
// does (#932): a 32B model on an 8 GB card runs mostly on the processor, so
// it isn't an excellent fit however much system memory there is. And on a
// computer without unified memory, the operating system keeps some.
func TestFitHoldsCardsToTheirMemory(t *testing.T) {
	catalog, err := NewCatalogEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	qwen32b, _ := catalog.Get("qwen2.5-32b-q4")
	qwen7b, _ := catalog.Get("qwen2.5-7b-q4")
	gemma9b, _ := catalog.Get("gemma-2-9b-q4")
	card := func(vram float64, backends ...string) contracts.HardwareInventory {
		return contracts.HardwareInventory{
			Memory:       contracts.MemoryInfo{TotalBytes: gib(32), AvailableBytes: gib(28)},
			Accelerators: []contracts.Accelerator{{Kind: "gpu", Vendor: "NVIDIA", DedicatedVRAM: gib(vram), Backends: backends}},
		}
	}

	fit := ClassifyFit(qwen32b, card(8, "cuda", "cpu"), "n", "PC", FitOptions{})
	if fitRank(fit.Label) > fitRank(contracts.FitTight) || fit.Short != "graphics" || fit.HaveBytes != gib(8) || fit.NeedBytes <= fit.HaveBytes {
		t.Errorf("32B on an 8 GB card: %s short=%q need %d have %d", fit.Label, fit.Short, fit.NeedBytes, fit.HaveBytes)
	}
	if !strings.Contains(fit.Reason, "graphics card has 8.0 GB") {
		t.Errorf("reason %q", fit.Reason)
	}
	if fit := ClassifyFit(qwen7b, card(8, "cuda", "cpu"), "n", "PC", FitOptions{}); fit.Label != contracts.FitExcellent || fit.Short != "" {
		t.Errorf("7B on an 8 GB card: %s short=%q", fit.Label, fit.Short)
	}
	// A card no backend can use doesn't hold the model.
	if fit := ClassifyFit(qwen32b, card(8, "cpu"), "n", "PC", FitOptions{}); fit.Short != "" {
		t.Errorf("32B with an unused card: short=%q", fit.Short)
	}
	// A model known to run keeps its label.
	if fit := ClassifyFit(qwen32b, card(8, "cuda"), "n", "PC", FitOptions{Proven: map[string]bool{qwen32b.ID: true}}); fit.Short != "" {
		t.Errorf("proven 32B: %s short=%q", fit.Label, fit.Short)
	}

	cpu8 := contracts.HardwareInventory{Memory: contracts.MemoryInfo{TotalBytes: gib(8), AvailableBytes: gib(6.4)}}
	if fit := ClassifyFit(gemma9b, cpu8, "n", "PC", FitOptions{}); fitRank(fit.Label) > fitRank(contracts.FitTight) {
		t.Errorf("Gemma 2 9B on 8 GB: %s", fit.Label)
	}
}

// Badges go to models that fit well, and Best overall is what onboarding
// picks (#932). On an 8 GB Mac, Best overall was Qwen 2.5 7B, which barely
// fits, while onboarding picked Llama 3.2 3B.
func TestWinnersFitWell(t *testing.T) {
	catalog, err := NewCatalogEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	presets, err := LoadPresetsEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	machines := map[string]contracts.HardwareInventory{
		"8 GB Mac":      apple(gib(8), gib(5)),
		"16 GB Mac":     apple(gib(16), gib(10)),
		"8 GB computer": {Memory: contracts.MemoryInfo{TotalBytes: gib(8), AvailableBytes: gib(6.4)}},
		"8 GB card": {
			Memory:       contracts.MemoryInfo{TotalBytes: gib(32), AvailableBytes: gib(28)},
			Accelerators: []contracts.Accelerator{{Kind: "gpu", DedicatedVRAM: gib(8), Backends: []string{"cuda", "cpu"}}},
		},
	}
	for name, hw := range machines {
		resp := BuildFitResponse(catalog, hw, "n", name, presets, FitOptions{})
		labels := map[string]contracts.FitLabel{}
		for _, f := range resp.Fits {
			labels[f.ModelID] = f.Label
		}
		rec, err := RecommendWithPresets(catalog, presets, RecommendInput{Purpose: "general", Hardware: hw})
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range resp.Winners {
			if l := labels[w.ModelID]; fitRank(l) < fitRank(contracts.FitTight) {
				t.Errorf("%s: %s is %s, which is %s", name, w.Label, w.ModelID, l)
			}
			if w.Category == "best_overall" {
				if l := labels[w.ModelID]; fitRank(l) < fitRank(contracts.FitGood) {
					t.Errorf("%s: Best overall is %s, which is %s", name, w.ModelID, l)
				}
				if w.ModelID != rec.Models[0].ID {
					t.Errorf("%s: Best overall is %s, onboarding picks %s", name, w.ModelID, rec.Models[0].ID)
				}
			}
		}
	}
}
