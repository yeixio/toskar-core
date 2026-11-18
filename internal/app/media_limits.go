package app

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/yeixio/toskar-core/internal/locale"
	"github.com/yeixio/toskar-core/internal/tools"
)

// mediaLimitNotice says, in the App language lang, what a picture or clip
// was made smaller or shorter than asked (#529), from the tool's
// "limited" result; "" when it's what was asked.
func mediaLimitNotice(lang, toolID string, result map[string]any) string {
	limited, _ := result["limited"].(map[string]any)
	if len(limited) == 0 {
		return ""
	}
	id := tools.Canonical(toolID)
	clip := id == "video.generate" || id == "video.extend"
	var parts []string
	if asked, ok := num(limited["asked_seconds"]); ok && clip {
		made, _ := num(limited["made_seconds"])
		most, _ := num(limited["max_seconds"])
		parts = append(parts, locale.T(lang, "chat:notices.mediaLength", map[string]any{
			"max": seconds(most), "made": seconds(made), "asked": seconds(asked)}))
	}
	if aw, ok := num(limited["asked_width"]); ok {
		ah, _ := num(limited["asked_height"])
		mw, _ := num(limited["made_width"])
		mh, _ := num(limited["made_height"])
		key := "chat:notices.mediaSizePicture"
		if clip {
			key = "chat:notices.mediaSizeClip"
		}
		model, _ := result["model"].(string)
		parts = append(parts, locale.T(lang, key, map[string]any{
			"model": model, "made": fmt.Sprintf("%.0f×%.0f", mw, mh), "asked": fmt.Sprintf("%.0f×%.0f", aw, ah)}))
	}
	if model, ok := limited["quality_model"].(string); ok && model != "" {
		used, _ := result["model"].(string)
		parts = append(parts, locale.T(lang, "chat:notices.mediaQuality", map[string]any{"model": model, "used": used}))
	}
	return strings.Join(parts, " ")
}

// num reads a number from a tool result, which is an int here and a
// float64 when it came from a paired computer.
func num(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

// seconds writes a length without a needless ".0".
func seconds(s float64) string {
	return strconv.FormatFloat(s, 'f', -1, 64)
}

// withMediaQuality adds the profile's picture and clip quality to a
// request that doesn't ask for one (#549).
func withMediaQuality(quality, toolID string, args map[string]any) map[string]any {
	id := tools.Canonical(toolID)
	if quality == "" || (id != "image.generate" && id != "video.generate" && id != "video.extend") {
		return args
	}
	if q, _ := args["quality"].(string); q != "" {
		return args
	}
	out := make(map[string]any, len(args)+1)
	for k, v := range args {
		out[k] = v
	}
	out["quality"] = quality
	return out
}
