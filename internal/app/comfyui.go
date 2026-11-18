package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/internal/api"
	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/comfyui"
	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/egress"
	"github.com/yeixio/toskar-core/internal/imagegen"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// comfyTokenSecret names the ComfyUI token in the secrets folder.
const comfyTokenSecret = "comfyui.token"

// comfyRunFor is the longest a workflow may run: clips on a busy ComfyUI
// take a while.
const comfyRunFor = 30 * time.Minute

func newComfyUI(cfg config.Config, secrets interface {
	Read(string) (string, error)
}) *comfyui.Client {
	base, _ := comfyui.NormalizeURL(cfg.ComfyUIURL)
	c := &comfyui.Client{}
	token, _ := secrets.Read(comfyTokenSecret)
	c.SetConfig(comfyui.Config{BaseURL: base, Token: strings.TrimSpace(token)})
	return c
}

// ComfyUI is the connected ComfyUI and what a check finds on it now.
func (a *App) ComfyUI(ctx context.Context) (api.ComfyUIStatus, error) {
	cfg := a.Comfy.Config()
	out := api.ComfyUIStatus{BaseURL: cfg.BaseURL, HasToken: cfg.Token != ""}
	if cfg.BaseURL == "" {
		return out, nil
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	info, err := a.Comfy.Check(cctx)
	if err != nil {
		out.Error = err.Error()
		return out, nil
	}
	out.Info = &info
	return out, nil
}

// SetComfyUI stores the address in the configuration and the token in the
// secrets folder, then checks it.
func (a *App) SetComfyUI(ctx context.Context, in api.ComfyUIInput) (api.ComfyUIStatus, error) {
	base, err := comfyui.NormalizeURL(in.BaseURL)
	if err != nil {
		return api.ComfyUIStatus{}, contracts.NewError("COMFYUI_BAD_URL", nil, err)
	}
	if err := a.Config.Update(func(c *config.Config) { c.ComfyUIURL = base }); err != nil {
		return api.ComfyUIStatus{}, err
	}
	cfg := a.Comfy.Config()
	cfg.BaseURL = base
	switch {
	case in.ClearToken || base == "":
		cfg.Token = ""
		_ = a.secrets.Delete(comfyTokenSecret)
	case strings.TrimSpace(in.Token) != "":
		cfg.Token = strings.TrimSpace(in.Token)
		if err := a.secrets.Write(comfyTokenSecret, cfg.Token); err != nil {
			return api.ComfyUIStatus{}, err
		}
	}
	a.Comfy.SetConfig(cfg)
	return a.ComfyUI(ctx)
}

func comfyView(w comfyui.Workflow) api.ComfyWorkflow {
	var graph map[string]json.RawMessage
	_ = json.Unmarshal(w.Graph, &graph)
	return api.ComfyWorkflow{Name: w.Name, Description: w.Description, Inputs: w.Inputs, Nodes: len(graph)}
}

// ComfyWorkflows lists the saved workflows.
func (a *App) ComfyWorkflows(context.Context) ([]api.ComfyWorkflow, error) {
	list, err := a.ComfyFlows.List()
	if err != nil {
		return nil, err
	}
	out := make([]api.ComfyWorkflow, 0, len(list))
	for _, w := range list {
		out = append(out, comfyView(w))
	}
	return out, nil
}

// DetectComfyWorkflow finds a workflow's usual inputs, to show before it's
// saved.
func (a *App) DetectComfyWorkflow(_ context.Context, graph json.RawMessage) (comfyui.Inputs, error) {
	in, err := comfyui.Detect(graph)
	if err != nil {
		return comfyui.Inputs{}, contracts.NewError("COMFYUI_BAD_WORKFLOW", nil, err)
	}
	return in, nil
}

// SaveComfyWorkflow saves a workflow under a clean name, detecting its
// inputs when none are given.
func (a *App) SaveComfyWorkflow(_ context.Context, in api.ComfyWorkflowInput) (api.ComfyWorkflow, error) {
	w := comfyui.Workflow{Name: comfyui.CleanName(in.Name), Description: strings.TrimSpace(in.Description), Graph: in.Graph}
	if in.Inputs != nil {
		w.Inputs = *in.Inputs
	} else {
		detected, err := comfyui.Detect(in.Graph)
		if err != nil {
			return api.ComfyWorkflow{}, contracts.NewError("COMFYUI_BAD_WORKFLOW", nil, err)
		}
		w.Inputs = detected
	}
	if w.Inputs.Prompt == nil && w.Inputs.Image == nil {
		return api.ComfyWorkflow{}, contracts.Errorf("COMFYUI_BAD_WORKFLOW", nil, "the workflow needs a prompt or a picture input, so a chat can tell it what to make")
	}
	if err := a.ComfyFlows.Save(w); err != nil {
		return api.ComfyWorkflow{}, contracts.NewError("COMFYUI_BAD_WORKFLOW", nil, err)
	}
	return comfyView(w), nil
}

// DeleteComfyWorkflow removes a saved workflow.
func (a *App) DeleteComfyWorkflow(_ context.Context, name string) error {
	return a.ComfyFlows.Delete(name)
}

// localHost reports an address on this computer, where a run doesn't
// leave it.
func localHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// comfyTool is comfyui.run: a workflow the Admin saved, run on their
// ComfyUI, with its pictures and clips attached to the answer.
type comfyTool struct {
	app *App
}

func (t *comfyTool) ID() string          { return "comfyui.run" }
func (t *comfyTool) DisplayName() string { return "ComfyUI Workflow" }
func (t *comfyTool) Description() string {
	return "Run a workflow saved for your ComfyUI, such as an upscale or a picture in a set style"
}

// Available reports a connected ComfyUI with a saved workflow.
func (t *comfyTool) Available() (bool, string) {
	if t.app.Comfy == nil || t.app.Comfy.Config().BaseURL == "" {
		return false, "No ComfyUI is connected. An Admin adds one in Tools › Images and video › ComfyUI."
	}
	if list, err := t.app.ComfyFlows.List(); err != nil || len(list) == 0 {
		return false, "No ComfyUI workflow is saved yet. An Admin adds one in Tools › Images and video › ComfyUI."
	}
	return true, ""
}

// workflow picks the named workflow, or the only one when none is named.
func (t *comfyTool) workflow(name string) (comfyui.Workflow, error) {
	list, err := t.app.ComfyFlows.List()
	if err != nil {
		return comfyui.Workflow{}, err
	}
	name = comfyui.CleanName(name)
	var names []string
	for _, w := range list {
		if w.Name == name || (name == "" && len(list) == 1) {
			return w, nil
		}
		desc := w.Name
		if w.Description != "" {
			desc += " (" + w.Description + ")"
		}
		names = append(names, desc)
	}
	if len(list) == 0 {
		return comfyui.Workflow{}, errors.New("no ComfyUI workflow is saved; an Admin adds one in Tools › Images and video › ComfyUI")
	}
	if name == "" {
		return comfyui.Workflow{}, fmt.Errorf("say which workflow as \"workflow\": %s", strings.Join(names, ", "))
	}
	return comfyui.Workflow{}, fmt.Errorf("no workflow named %q; the saved ones are %s", name, strings.Join(names, ", "))
}

func (t *comfyTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	if ok, why := t.Available(); !ok {
		return nil, errors.New(why)
	}
	name, _ := args["workflow"].(string)
	w, err := t.workflow(name)
	if err != nil {
		return nil, err
	}
	host := t.app.Comfy.Host()
	remote := !localHost(host)
	if remote && t.offline(ctx) {
		return nil, fmt.Errorf("this profile keeps work on this computer, and the ComfyUI is on %s; use the built-in image tools or another profile", host)
	}
	prompt, _ := args["prompt"].(string)
	negative, _ := args["negative"].(string)
	v := comfyui.Values{Prompt: strings.TrimSpace(prompt), Negative: strings.TrimSpace(negative), Seed: int64(numArg(args["seed"])),
		Width: int(numArg(args["width"])), Height: int(numArg(args["height"]))}
	var pic *comfyui.Picture
	from := ""
	if ref, _ := args["file"].(string); strings.TrimSpace(ref) != "" {
		a, data, err := imagegen.FindImage(ctx, t.app.Artifacts, strings.TrimSpace(ref))
		if err != nil {
			return nil, err
		}
		pic, from = &comfyui.Picture{Name: a.Name, Data: data}, a.Name
	}
	if remote {
		what := "prompt for the " + w.Name + " workflow"
		if pic != nil {
			what = "picture " + from + " and " + what
		}
		t.app.Egress.Add(ctx, egress.ComfyUI, host, what)
	}
	rctx, cancel := context.WithTimeout(ctx, comfyRunFor)
	defer cancel()
	start := time.Now()
	files, seed, err := t.app.Comfy.RunWorkflow(rctx, w, v, pic)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"workflow": w.Name, "seconds": time.Since(start).Round(time.Second).Seconds()}
	if seed > 0 {
		result["seed"] = seed
	}
	if v.Prompt != "" {
		result["prompt"] = v.Prompt
	}
	details := map[string]any{"workflow": w.Name, "made_with": "ComfyUI"}
	if v.Prompt != "" {
		details["prompt"] = v.Prompt
	}
	var saved []map[string]any
	for _, f := range files {
		a, err := t.app.Artifacts.Save(ctx, artifacts.Input{ConversationID: artifacts.ConversationFrom(ctx), Name: artifacts.CleanName(comfyName(w.Name, f.Name)),
			Producer: artifacts.ProducerAssistant, Data: f.Data, Details: details})
		if err != nil {
			return nil, err
		}
		saved = append(saved, map[string]any{"id": a.ID, "name": a.Name, "kind": a.Kind})
	}
	result["files"] = saved
	result["note"] = "The files are attached to your answer, where they are shown. Do not describe them as if you can see them."
	return result, nil
}

// offline is whether the chat's profile keeps work on this computer.
func (t *comfyTool) offline(ctx context.Context) bool {
	id := chatProfileFrom(ctx)
	if id == "" || t.app.Profiles == nil {
		return false
	}
	p, err := t.app.Profiles.Get(ctx, id)
	return err == nil && profileOffline(p)
}

// comfyName names a file for its workflow, keeping ComfyUI's number and
// type: upscale-00001.png.
func comfyName(workflow, name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	base := strings.TrimRight(strings.TrimSuffix(name, filepath.Ext(name)), "_-")
	i := len(base)
	for i > 0 && base[i-1] >= '0' && base[i-1] <= '9' {
		i--
	}
	if i < len(base) {
		return workflow + "-" + base[i:] + ext
	}
	return workflow + ext
}

func numArg(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return 0
}
