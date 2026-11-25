package imagegen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/remotetools"
	"github.com/yeixio/toskar-core/internal/webm"
)

// MaxShots is the most shots a longer video has: two minutes of 5-second
// shots. Each shot takes minutes to make, and the joined clip has to fit
// in a reply from a paired computer.
const MaxShots = 24

// Shot is one scene of a longer video.
type Shot struct {
	Prompt  string  `json:"prompt"`
	Seconds float64 `json:"seconds,omitempty"`
}

// ShotsArg reads the "shots" argument: a list of scenes, each a prompt or
// {"prompt", "seconds"}. Shots without a length take 5 seconds.
func ShotsArg(v any) ([]Shot, error) {
	list, _ := v.([]any)
	if len(list) == 0 {
		return nil, errors.New(`shots required: a list of scenes, each {"prompt": "...", "seconds": 5}`)
	}
	if len(list) > MaxShots {
		return nil, fmt.Errorf("a longer video has at most %d shots, not %d", MaxShots, len(list))
	}
	out := make([]Shot, 0, len(list))
	for i, item := range list {
		var s Shot
		switch x := item.(type) {
		case string:
			s.Prompt = x
		case map[string]any:
			s.Prompt, _ = x["prompt"].(string)
			s.Seconds, _ = x["seconds"].(float64)
		}
		s.Prompt = strings.TrimSpace(s.Prompt)
		if s.Prompt == "" {
			return nil, fmt.Errorf("shot %d has no prompt: say what happens in it", i+1)
		}
		if s.Seconds <= 0 {
			s.Seconds = maxSeconds
		}
		s.Seconds = math.Min(math.Max(s.Seconds, 1), maxSeconds)
		out = append(out, s)
	}
	return out, nil
}

// withStyle adds the shared style line to a shot's prompt, so faces,
// clothes, and color drift less from shot to shot.
func withStyle(prompt, style string) string {
	style = strings.TrimSpace(style)
	if style == "" {
		return prompt
	}
	return strings.TrimRight(prompt, ". ") + ". " + style
}

// ShotsTool is video.shots: a longer video made as a series of shots
// (#790). The models make clips of a few seconds, so each shot is one
// clip, started from the last frame of the one before so it flows, and
// the shots are joined in order into one clip.
type ShotsTool struct {
	Engine *Engine
	Store  *artifacts.Store
}

func (t *ShotsTool) ID() string                     { return "video.shots" }
func (t *ShotsTool) DisplayName() string            { return "Make a Longer Video" }
func (t *ShotsTool) Description() string            { return "Make a longer video as a series of shots" }
func (t *ShotsTool) Available() (bool, string)      { return t.Engine.Available() }
func (t *ShotsTool) Provider() remotetools.Provider { return t.Engine.provider(t.ID()) }
func (t *ShotsTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	return remotetools.Execute(ctx, t, args)
}

// Prepare checks the shots and reads the picture the first one starts
// from, if there is one.
func (t *ShotsTool) Prepare(ctx context.Context, args map[string]any) (remotetools.Job, error) {
	if _, err := ShotsArg(args["shots"]); err != nil {
		return remotetools.Job{}, err
	}
	ref, _ := args["image"].(string)
	if ref = strings.TrimSpace(ref); ref == "" {
		return remotetools.Job{Args: args}, nil
	}
	a, data, err := findImage(ctx, t.Store, ref)
	if err != nil {
		return remotetools.Job{}, err
	}
	return remotetools.Job{Args: args, Files: []remotetools.File{{Name: a.Name, Data: data}}}, nil
}

// Run makes the shots one after another and joins them. Between shots the
// engine is free, so a picture asked for meanwhile doesn't wait for the
// whole video. When a shot fails after others were made, the video so far
// is kept and the result says where it stopped.
func (t *ShotsTool) Run(ctx context.Context, job remotetools.Job) (remotetools.Output, error) {
	shots, err := ShotsArg(job.Args["shots"])
	if err != nil {
		return remotetools.Output{}, err
	}
	style, _ := job.Args["style"].(string)
	seed := int64(intArg(job.Args["seed"]))
	quality := qualityArg(job.Args["quality"])
	req := VideoRequest{Width: intArg(job.Args["width"]), Height: intArg(job.Args["height"]), Quality: quality}
	if len(job.Files) == 1 {
		req.Image, req.ImageName = job.Files[0].Data, job.Files[0].Name
	}
	report := progressFrom(ctx)
	var (
		whole   *webm.Clip
		joined  []byte
		made    []map[string]any
		spent   float64
		last    VideoResult
		stopped string
	)
	for i, shot := range shots {
		if i > 0 {
			frame, err := whole.LastFrame()
			if err != nil {
				return remotetools.Output{}, fmt.Errorf("shot %d can't start from shot %d: %w", i+1, i, err)
			}
			var b bytes.Buffer
			if err := png.Encode(&b, frame); err != nil {
				return remotetools.Output{}, err
			}
			req.Image, req.ImageName = b.Bytes(), "last-frame.png"
			req.Width, req.Height = whole.Width, whole.Height
		}
		req.Prompt, req.Seconds = withStyle(shot.Prompt, style), shot.Seconds
		req.Seed = 0
		if seed > 0 {
			req.Seed = seed + int64(i)
		}
		shotCtx := ctx
		if report != nil {
			n := i + 1
			shotCtx = WithProgress(ctx, func(p Progress) {
				p.Shot, p.Shots = n, len(shots)
				report(p)
			})
		}
		res, err := t.Engine.GenerateVideo(shotCtx, req)
		if err == nil {
			var part *webm.Clip
			if part, err = webm.Parse(res.WebM); err == nil {
				if whole == nil {
					joined, whole = res.WebM, part
				} else if joined, err = webm.Join(whole, part, true); err == nil {
					whole, err = webm.Parse(joined)
				}
			}
		}
		if err != nil {
			// Nothing made yet, or stopped by the person: no video.
			if i == 0 {
				return remotetools.Output{}, fmt.Errorf("shot 1: %w", err)
			}
			if ctx.Err() != nil {
				return remotetools.Output{}, err
			}
			stopped = fmt.Sprintf("shot %d failed: %v", i+1, err)
			break
		}
		last, spent = res, spent+res.Seconds
		made = append(made, map[string]any{"prompt": shot.Prompt, "seconds": res.Length, "seed": res.Seed})
	}
	name, _ := job.Args["name"].(string)
	if !nameable(name) {
		name = ""
	}
	name = artifacts.CleanName(strings.TrimSpace(name))
	if name == "" || name == "file" {
		if name = nameFrom(shots[0].Prompt); name == "image" {
			name = "video"
		}
	}
	name = strings.TrimSuffix(name, filepath.Ext(name)) + ".webm"
	result := map[string]any{"width": whole.Width, "height": whole.Height, "frames": len(whole.Frames),
		"length_seconds": math.Round(whole.Seconds()*10) / 10, "shots": made, "model": last.Model,
		"seconds": math.Round(spent), "shots_made": len(made),
		// The last shot's prompt, so Continue Video goes on from the end.
		"prompt": withStyle(shots[len(made)-1].Prompt, style)}
	if style = strings.TrimSpace(style); style != "" {
		result["style"] = style
	}
	if last.Tier != "" {
		result["quality"] = last.Tier
	}
	if stopped != "" {
		result["stopped"] = stopped
		result["shots_planned"] = len(shots)
	}
	return remotetools.Output{Result: result, Files: []remotetools.File{{Name: name, Data: joined}}}, nil
}

// Finish attaches the video to the chat.
func (t *ShotsTool) Finish(ctx context.Context, out remotetools.Output) (map[string]any, error) {
	res, err := finishClip(ctx, t.Store, out)
	if err != nil {
		return nil, err
	}
	res["note"] = "The video is attached to your answer, where it plays. Do not describe it as if you can see it. Each shot's seed makes it again."
	if stopped, _ := res["stopped"].(string); stopped != "" {
		res["note"] = fmt.Sprintf("Only %v of %v shots were made (%s). Say so, and that the rest can be added with video.extend from this clip. ", res["shots_made"], res["shots_planned"], stopped) + res["note"].(string)
	}
	return res, nil
}

// clipRate is how long this computer took to make a second of clip, the
// last time it made one, for estimating a longer video before it starts.
type clipRate struct {
	Model            string  `json:"model"`
	SecondsPerSecond float64 `json:"seconds_per_second"`
}

var rateMu sync.Mutex

func (e *Engine) rateFile() string { return filepath.Join(e.WorkDir, "clip-rate.json") }

// noteRate keeps how long a clip took, per second of it.
func (e *Engine) noteRate(model string, took, length float64) {
	if e.WorkDir == "" || took <= 0 || length <= 0 {
		return
	}
	data, err := json.Marshal(clipRate{Model: model, SecondsPerSecond: took / length})
	if err != nil {
		return
	}
	rateMu.Lock()
	defer rateMu.Unlock()
	if os.MkdirAll(e.WorkDir, 0o700) == nil {
		_ = os.WriteFile(e.rateFile(), data, 0o600)
	}
}

// ShotsLength is how long the shots come to, in seconds.
func ShotsLength(shots []Shot) float64 {
	var total float64
	for _, s := range shots {
		total += s.Seconds
	}
	return total
}

// Estimate is about how long seconds of video take to make here, from the
// last clip this computer made; 0 when it hasn't made one yet.
func (e *Engine) Estimate(seconds float64) time.Duration {
	if e == nil || e.WorkDir == "" {
		return 0
	}
	rateMu.Lock()
	data, err := os.ReadFile(e.rateFile())
	rateMu.Unlock()
	var r clipRate
	if err != nil || json.Unmarshal(data, &r) != nil || r.SecondsPerSecond <= 0 {
		return 0
	}
	return time.Duration(seconds * r.SecondsPerSecond * float64(time.Second))
}

// MaxClipSeconds is the most one clip can be; a longer video is made as
// shots of up to this length.
const MaxClipSeconds = maxSeconds

// ShotsFor plans a video of seconds as shots of one prompt (#1382): one
// clip's length each, the last taking what's left, at most MaxShots. Each
// shot goes on from the one before, so one prompt makes one long scene.
// made is how long the shots come to, less than seconds past MaxShots.
func ShotsFor(prompt string, seconds float64) (shots []any, made float64) {
	seconds = math.Min(seconds, MaxShots*maxSeconds)
	for left := seconds; left > 0 && len(shots) < MaxShots; left -= maxSeconds {
		s := math.Max(math.Min(left, maxSeconds), 1)
		shots = append(shots, map[string]any{"prompt": prompt, "seconds": s})
		made += s
	}
	return shots, made
}
