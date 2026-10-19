package imagegen

import (
	"strings"
	"testing"
)

// The working-set estimate is calibrated with the graphics memory jobs
// used on the M5 Pro (2026-10-19, #550, #553): "In use system memory"
// above idle, sampled every 0.2 s. It must never be below a measured run,
// and not more than 1.6 GB above it, except where a part decoded in
// chunks and used less than it would have (chunked).
func TestGraphicsForMatchesMeasuredRuns(t *testing.T) {
	klein := mustModel(t, "flux2-klein-4b-q8")
	klein4 := mustModel(t, "flux2-klein-4b")
	zimage := mustModel(t, "z-image-turbo")
	wan := mustModel(t, "wan2.2-ti2v-5b")
	wan21 := mustModel(t, "wan2.1-t2v-1.3b")
	for _, c := range []struct {
		name     string
		m        Model
		w, h, n  int
		measured float64 // GB
		chunked  bool
	}{
		{"FLUX.2 klein Q8 512×512", klein, 512, 512, 1, 4.96, false},
		{"FLUX.2 klein Q8 1024×1024", klein, 1024, 1024, 1, 5.91, false},
		{"FLUX.2 klein Q8 1536×1024", klein, 1536, 1024, 1, 8.46, false},
		{"FLUX.2 klein Q4 512×512", klein4, 512, 512, 1, 2.37, false},
		{"FLUX.2 klein Q4 1024×1024", klein4, 1024, 1024, 1, 5.39, false},
		{"FLUX.2 klein Q4 1536×1024", klein4, 1536, 1024, 1, 8.36, false},
		{"Z-Image-Turbo 512×512", zimage, 512, 512, 1, 5.02, false},
		{"Z-Image-Turbo 1024×1024", zimage, 1024, 1024, 1, 6.68, false},
		{"Z-Image-Turbo 1536×1024", zimage, 1536, 1024, 1, 10.85, false},
		{"Wan 2.2 5B 9 frames", wan, 832, 480, 9, 3.98, false},
		{"Wan 2.2 5B 17 frames", wan, 832, 480, 17, 4.24, false},
		{"Wan 2.1 1.3B 9 frames", wan21, 832, 480, 9, 8.15, false},
		{"Wan 2.1 1.3B 17 frames", wan21, 832, 480, 17, 14.11, false},
		{"Wan 2.1 1.3B 33 frames", wan21, 832, 480, 33, 11.00, true},
	} {
		got := float64(c.m.GraphicsFor(c.w, c.h, c.n)) / 1e9
		if got < c.measured || (!c.chunked && got > c.measured+1.6) {
			t.Errorf("%s: estimated %.2f GB, measured %.2f GB", c.name, got, c.measured)
		}
	}
	// The setup cards judge the default request: a 1024×1024 picture, a
	// 2-second clip.
	if klein.GraphicsBytes != klein.GraphicsFor(1024, 1024, 1) || wan.GraphicsBytes != wan.GraphicsFor(832, 480, 33) {
		t.Errorf("default graphics: klein %d, wan %d", klein.GraphicsBytes, wan.GraphicsBytes)
	}
	// A longer clip needs more: the decoder holds every frame.
	if wan.GraphicsFor(832, 480, 81) <= wan.GraphicsFor(832, 480, 33) {
		t.Error("81 frames need no more than 33")
	}
}

// A request too big for this computer's graphics is turned down with what
// fits; one that fits, or with graphics memory unknown, isn't.
func TestTooBigForSaysWhatFits(t *testing.T) {
	klein := mustModel(t, "flux2-klein-4b-q8")
	wan := mustModel(t, "wan2.2-ti2v-5b")
	const gib = int64(1) << 30
	if why := tooBigFor(klein, 1024, 1024, 1, 16*gib); why != "" {
		t.Errorf("fits in 16 GB: %q", why)
	}
	if why := tooBigFor(klein, 1536, 1024, 1, 0); why != "" {
		t.Errorf("unknown graphics: %q", why)
	}
	if why := tooBigFor(klein, 1536, 1024, 1, 7*gib); !strings.Contains(why, "Ask for") || !strings.Contains(why, "or smaller") {
		t.Errorf("1536×1024 in 7 GB: %q", why)
	}
	why := tooBigFor(wan, 832, 480, 81, 8*gib)
	if !strings.Contains(why, "seconds or less") {
		t.Fatalf("5 s in 8 GB: %q", why)
	}
	// The length it suggests fits.
	if wan.GraphicsFor(832, 480, 33) > 8*gib && strings.Contains(why, "Ask for 2 seconds") {
		t.Errorf("suggests a length that doesn't fit: %q", why)
	}
	// A model without measured stages keeps its one estimate, and isn't turned down per request.
	plain := Model{Name: "Plain", Kind: "image", GraphicsBytes: 9 * gib}
	if plain.GraphicsFor(1536, 1024, 1) != 9*gib || tooBigFor(plain, 1536, 1024, 1, 4*gib) != "" {
		t.Error("a model without stages")
	}
}

func mustModel(t *testing.T, id string) Model {
	t.Helper()
	for _, m := range append(Catalog(), VideoCatalog()...) {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("%s not in the media manifest", id)
	return Model{}
}
