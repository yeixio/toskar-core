package imagegen

import (
	"fmt"
	"math"
)

// Stage is one part of a model as sd-cli runs it with --offload-to-cpu:
// the text encoder, the diffusion model, then the decoder, each moved to
// the graphics memory in turn with its working buffers (#550). Measured
// from sd-cli's own buffer sizes on the M5 Pro (2026-10-19).
type Stage struct {
	// Part names it: text, diffusion, or decode.
	Part string `json:"part"`
	// WeightsBytes is its weights, and ComputeBytes its buffers whatever
	// the size; PerMegapixelBytes is what each megapixel of each frame adds.
	WeightsBytes      int64 `json:"weights_bytes"`
	ComputeBytes      int64 `json:"compute_bytes,omitempty"`
	PerMegapixelBytes int64 `json:"per_megapixel_bytes,omitempty"`
	// MaxComputeBytes caps the buffers of a part that works in chunks when
	// they'd grow too large, such as Wan 2.1's TAEHV decoder: the most it
	// was seen to take (#553). 0 is no cap.
	MaxComputeBytes int64 `json:"max_compute_bytes,omitempty"`
}

// stageOverhead is what the graphics hold beyond the largest stage: the
// backend's own buffers and what the stage before leaves. Measured as
// 0.24–0.48 GB on the M5 Pro for FLUX.2 [klein] and Wan 2.2 (#550), and
// up to 0.97 GB for Z-Image-Turbo at 512×512 (#553).
const stageOverhead = 1 << 30

// GraphicsFor is the graphics memory m needs to make a picture or clip of
// w×h pixels and this many frames (1 for a picture): its largest stage,
// and the overhead. A model without measured stages has only its one
// estimate, GraphicsBytes.
func (m Model) GraphicsFor(w, h, n int) int64 {
	if len(m.Stages) == 0 {
		return m.GraphicsBytes
	}
	pixels := float64(w) * float64(h) / 1e6 * float64(max(n, 1))
	var most int64
	for _, s := range m.Stages {
		compute := s.ComputeBytes + int64(math.Ceil(float64(s.PerMegapixelBytes)*pixels))
		if s.MaxComputeBytes > 0 {
			compute = min(compute, s.MaxComputeBytes)
		}
		most = max(most, s.WeightsBytes+compute)
	}
	return most + stageOverhead
}

// defaultRequest is the picture or clip a request gets when it asks for no
// size or length: what the setup cards' verdicts are for.
func (m Model) defaultRequest() (w, h, n int) {
	if m.Kind == "video" {
		w, h = videoSize(0, 0, nil)
		return w, h, frames(0, m.FPS)
	}
	return defaultSide, defaultSide, 1
}

// tooBigFor says what fits instead when a request needs more graphics
// memory than this computer's graphics can use, and "" when it fits or
// the graphics memory isn't known (#550).
func tooBigFor(m Model, w, h, n int, graphics int64) string {
	need := m.GraphicsFor(w, h, n)
	if graphics <= 0 || len(m.Stages) == 0 || need <= graphics {
		return ""
	}
	if m.Kind == "video" {
		fps := max(m.FPS, 1)
		for fewer := n - 4; fewer >= 9; fewer -= 4 {
			if m.GraphicsFor(w, h, fewer) <= graphics {
				return fmt.Sprintf("A clip that long needs about %.0f GB of graphics memory, and this computer's graphics can use %.0f GB. Ask for %.0f seconds or less, or make it on a paired computer with more", gb(need), gb(graphics), math.Floor(float64(fewer-1)/float64(fps)))
			}
		}
		return fmt.Sprintf("%s needs about %.0f GB of graphics memory even for a short clip, and this computer's graphics can use %.0f GB; a paired computer with more can make it", m.Name, gb(m.GraphicsFor(w, h, 9)), gb(graphics))
	}
	for side := 1024; side >= minSide; side -= 128 {
		scale := math.Min(1, float64(side)/float64(max(w, h)))
		sw, sh := fit(int(float64(w)*scale), int(float64(h)*scale))
		if m.GraphicsFor(sw, sh, 1) <= graphics {
			return fmt.Sprintf("A picture that size needs about %.0f GB of graphics memory, and this computer's graphics can use %.0f GB. Ask for %d×%d or smaller, or make it on a paired computer with more", gb(need), gb(graphics), sw, sh)
		}
	}
	return fmt.Sprintf("%s needs about %.0f GB of graphics memory, and this computer's graphics can use %.0f GB; a paired computer with more can make it", m.Name, gb(m.GraphicsFor(minSide, minSide, 1)), gb(graphics))
}
