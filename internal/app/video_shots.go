package app

import (
	"math"

	"github.com/yeixio/toskar-core/internal/imagegen"
)

// shotsEstimate is what a longer video's approval card shows besides its
// shots (#790): how long the video is, and about how long it takes to make
// on this computer, from the last clip made here. The minutes are left out
// until this computer has made a clip.
func (a *App) shotsEstimate(args map[string]any) map[string]any {
	shots, err := imagegen.ShotsArg(args["shots"])
	if err != nil {
		return nil
	}
	length := imagegen.ShotsLength(shots)
	out := map[string]any{"video_seconds": length}
	if took := a.videoEngine.Estimate(length); took > 0 {
		out["estimate_minutes"] = int(math.Max(1, math.Round(took.Minutes())))
	}
	return out
}
