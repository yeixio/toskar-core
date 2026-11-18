package imagegen

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"math"
	"path/filepath"
	"strings"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/remotetools"
	"github.com/yeixio/toskar-core/internal/webm"
)

// ExtendTool is video.extend: a clip in the chat, continued from its last
// frame and joined into one longer clip (#155). Each part is a clip of a
// few seconds, so longer videos are made a part at a time.
type ExtendTool struct {
	Engine *Engine
	Store  *artifacts.Store
}

func (t *ExtendTool) ID() string                     { return "video.extend" }
func (t *ExtendTool) DisplayName() string            { return "Continue Video" }
func (t *ExtendTool) Description() string            { return "Continue a clip in this chat, from its last frame" }
func (t *ExtendTool) Available() (bool, string)      { return t.Engine.Available() }
func (t *ExtendTool) Provider() remotetools.Provider { return t.Engine.provider(t.ID()) }
func (t *ExtendTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	return remotetools.Execute(ctx, t, args)
}

// isClip reports a WebM file's name.
func isClip(name string) bool { return strings.EqualFold(filepath.Ext(name), ".webm") }

// findClip returns a clip by id, or the newest one in this chat with that
// name, or the newest clip when ref is empty.
func findClip(ctx context.Context, store *artifacts.Store, ref string) (artifacts.Artifact, []byte, error) {
	if ref != "" {
		if a, data, err := store.Read(ctx, ref); err == nil && isClip(a.Name) {
			return a, data, nil
		}
	}
	list, err := store.List(ctx, artifacts.ConversationFrom(ctx))
	if err != nil {
		return artifacts.Artifact{}, nil, err
	}
	for i := len(list) - 1; i >= 0; i-- {
		if isClip(list[i].Name) && (ref == "" || strings.EqualFold(list[i].Name, ref)) {
			return store.Read(ctx, list[i].ID)
		}
	}
	if ref == "" {
		return artifacts.Artifact{}, nil, errors.New("there's no clip in this chat to continue")
	}
	return artifacts.Artifact{}, nil, fmt.Errorf("no clip named %q in this chat", ref)
}

// Prepare reads the clip. Without a prompt, the clip's own goes on.
func (t *ExtendTool) Prepare(ctx context.Context, args map[string]any) (remotetools.Job, error) {
	ref, _ := args["clip"].(string)
	a, data, err := findClip(ctx, t.Store, strings.TrimSpace(ref))
	if err != nil {
		return remotetools.Job{}, err
	}
	job := map[string]any{}
	for k, v := range args {
		job[k] = v
	}
	if p, _ := job["prompt"].(string); strings.TrimSpace(p) == "" {
		if before, _ := a.Details["prompt"].(string); before != "" {
			job["prompt"] = before
		}
	}
	return remotetools.Job{Args: job, Files: []remotetools.File{{Name: a.Name, Data: data}}}, nil
}

// Run makes the next part from the clip's last frame and joins the two.
func (t *ExtendTool) Run(ctx context.Context, job remotetools.Job) (remotetools.Output, error) {
	if len(job.Files) != 1 {
		return remotetools.Output{}, errors.New("no clip to continue")
	}
	from := job.Files[0]
	clip, err := webm.Parse(from.Data)
	if err != nil {
		return remotetools.Output{}, fmt.Errorf("%s can't be continued: %w", from.Name, err)
	}
	last, err := clip.LastFrame()
	if err != nil {
		return remotetools.Output{}, fmt.Errorf("%s can't be continued: %w", from.Name, err)
	}
	var frame bytes.Buffer
	if err := png.Encode(&frame, last); err != nil {
		return remotetools.Output{}, err
	}
	prompt, _ := job.Args["prompt"].(string)
	if strings.TrimSpace(prompt) == "" {
		return remotetools.Output{}, errors.New("prompt required: say what happens next")
	}
	seconds, _ := job.Args["seconds"].(float64)
	res, err := t.Engine.GenerateVideo(ctx, VideoRequest{Prompt: prompt, Width: clip.Width, Height: clip.Height, Seconds: seconds,
		Seed: int64(intArg(job.Args["seed"])), Image: frame.Bytes(), ImageName: "last-frame.png", Quality: qualityArg(job.Args["quality"])})
	if err != nil {
		return remotetools.Output{}, err
	}
	part, err := webm.Parse(res.WebM)
	if err != nil {
		return remotetools.Output{}, fmt.Errorf("the new part: %w", err)
	}
	joined, err := webm.Join(clip, part, true)
	if err != nil {
		return remotetools.Output{}, err
	}
	whole, err := webm.Parse(joined)
	if err != nil {
		return remotetools.Output{}, fmt.Errorf("the joined clip: %w", err)
	}
	name, _ := job.Args["name"].(string)
	if !nameable(name) {
		name = ""
	}
	name = artifacts.CleanName(strings.TrimSpace(name))
	if name == "" || name == "file" {
		name = strings.TrimSuffix(from.Name, filepath.Ext(from.Name)) + "-longer"
	}
	name = strings.TrimSuffix(name, filepath.Ext(name)) + ".webm"
	result := map[string]any{"width": whole.Width, "height": whole.Height, "frames": len(whole.Frames),
		"length_seconds": math.Round(whole.Seconds()*10) / 10, "added_seconds": res.Length, "continued": from.Name,
		"seed": res.Seed, "model": res.Model, "seconds": math.Round(res.Seconds), "prompt": prompt}
	if res.Tier != "" {
		result["quality"] = res.Tier
	}
	if l := withMissing(limits(0, 0, res.Width, res.Height, seconds, res.Length), res.Missing); l != nil {
		result["limited"] = l
	}
	return remotetools.Output{Result: result, Files: []remotetools.File{{Name: name, Data: joined}}}, nil
}

// Finish attaches the longer clip to the chat.
func (t *ExtendTool) Finish(ctx context.Context, out remotetools.Output) (map[string]any, error) {
	return finishClip(ctx, t.Store, out)
}
