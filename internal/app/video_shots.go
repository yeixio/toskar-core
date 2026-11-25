package app

import (
	"math"
	"strings"

	"github.com/yeixio/toskar-core/internal/imagegen"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
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

// asLongerVideo makes a clip asked for longer than one clip can be as a
// longer video of shots (#1382), when the profile has video.shots: the
// person gets the length they asked for, joined from clips, instead of a
// clip cut to 5 seconds. The length is the person's own words, or the
// model's when they named none. limited is what a length past the most a
// longer video can be was made at, for the answer to say; nil otherwise.
func asLongerVideo(profile contracts.AIProfile, message, toolID string, args map[string]any) (string, map[string]any, map[string]any) {
	if tools.Canonical(toolID) != "video.generate" || tools.PolicyForProfile(profile, "video.shots") == tools.PolicyDeny {
		return toolID, args, nil
	}
	asked := askedSeconds(message)
	if asked <= 0 {
		asked, _ = num(args["seconds"])
	}
	prompt, _ := args["prompt"].(string)
	if asked <= imagegen.MaxClipSeconds || strings.TrimSpace(prompt) == "" {
		return toolID, args, nil
	}
	shots, made := imagegen.ShotsFor(strings.TrimSpace(prompt), asked)
	out := map[string]any{"shots": shots}
	for _, k := range []string{"image", "width", "height", "seed", "quality", "name"} {
		if v, ok := args[k]; ok {
			out[k] = v
		}
	}
	var limited map[string]any
	if made < asked {
		limited = map[string]any{"asked_seconds": asked, "made_seconds": made, "max_seconds": made}
	}
	return "video.shots", out, limited
}
