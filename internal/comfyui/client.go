// Package comfyui connects Toskar to a ComfyUI the Admin runs, here or on
// another computer (#958): it checks it, and runs saved workflows on it as
// picture and clip tools beside the built-in engine. Toskar never installs
// anything into ComfyUI; custom nodes run code on that computer, so only a
// ComfyUI the Admin added is called.
package comfyui

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Config is where ComfyUI is, and its token if it has one.
type Config struct {
	BaseURL string
	Token   string
}

// Client calls one ComfyUI.
type Client struct {
	mu   sync.RWMutex
	cfg  Config
	HTTP *http.Client
	// Poll is how often a running workflow is checked; zero is a second.
	Poll time.Duration
}

// NormalizeURL checks an address such as http://127.0.0.1:8188 and drops a
// trailing slash. Empty is no ComfyUI.
func NormalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" {
		return "", errors.New("the address must start with http:// or https://, such as http://127.0.0.1:8188")
	}
	return strings.TrimRight(raw, "/"), nil
}

func (c *Client) Config() Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cfg
}

func (c *Client) SetConfig(cfg Config) {
	c.mu.Lock()
	c.cfg = cfg
	c.mu.Unlock()
}

// Host is where requests go, for What left this computer.
func (c *Client) Host() string {
	if u, err := url.Parse(c.Config().BaseURL); err == nil {
		return u.Host
	}
	return ""
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

// ErrNotSetUp is a call without a ComfyUI address.
var ErrNotSetUp = errors.New("no ComfyUI is connected; an Admin adds one in Tools › Images and video › ComfyUI")

func (c *Client) do(ctx context.Context, method, path, contentType string, body io.Reader) (*http.Response, error) {
	cfg := c.Config()
	if cfg.BaseURL == "" {
		return nil, ErrNotSetUp
	}
	req, err := http.NewRequestWithContext(ctx, method, cfg.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("ComfyUI at %s didn't answer: %w", c.Host(), err)
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("ComfyUI at %s refused the token (%d)", c.Host(), resp.StatusCode)
		}
		return nil, fmt.Errorf("ComfyUI at %s answered %d: %s", c.Host(), resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return resp, nil
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	resp, err := c.do(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(out)
}

// Device is one of ComfyUI's processors.
type Device struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	VRAMTotal int64  `json:"vram_total"`
	VRAMFree  int64  `json:"vram_free"`
}

// Info is what a check finds: ComfyUI's version, its graphics, and the
// models it has, by kind.
type Info struct {
	Version     string              `json:"version,omitempty"`
	Devices     []Device            `json:"devices"`
	Models      map[string][]string `json:"models"`
	CustomNodes int                 `json:"node_types"`
}

// modelInputs are the loader nodes whose choices are the models ComfyUI
// has, by the kind Toskar shows them as.
var modelInputs = map[string][2]string{
	"checkpoints": {"CheckpointLoaderSimple", "ckpt_name"},
	"diffusion":   {"UNETLoader", "unet_name"},
	"loras":       {"LoraLoader", "lora_name"},
	"vae":         {"VAELoader", "vae_name"},
	"upscalers":   {"UpscaleModelLoader", "model_name"},
}

// Check reads /system_stats and /object_info.
func (c *Client) Check(ctx context.Context) (Info, error) {
	var stats struct {
		System struct {
			ComfyUIVersion string `json:"comfyui_version"`
		} `json:"system"`
		Devices []Device `json:"devices"`
	}
	if err := c.getJSON(ctx, "/system_stats", &stats); err != nil {
		return Info{}, err
	}
	var objects map[string]struct {
		Input struct {
			Required map[string]json.RawMessage `json:"required"`
		} `json:"input"`
	}
	if err := c.getJSON(ctx, "/object_info", &objects); err != nil {
		return Info{}, err
	}
	info := Info{Version: stats.System.ComfyUIVersion, Devices: stats.Devices, Models: map[string][]string{}, CustomNodes: len(objects)}
	if info.Devices == nil {
		info.Devices = []Device{}
	}
	for kind, at := range modelInputs {
		node, ok := objects[at[0]]
		if !ok {
			continue
		}
		// An input's choices are [[names...], {options}].
		var choice []json.RawMessage
		if json.Unmarshal(node.Input.Required[at[1]], &choice) != nil || len(choice) == 0 {
			continue
		}
		var names []string
		if json.Unmarshal(choice[0], &names) == nil && len(names) > 0 {
			sort.Strings(names)
			info.Models[kind] = names
		}
	}
	return info, nil
}

// Output is a file a workflow made.
type Output struct {
	Name string
	Data []byte
}

// upload sends a picture for a workflow's LoadImage node and returns the
// name ComfyUI keeps it under.
func (c *Client) upload(ctx context.Context, name string, data []byte) (string, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("image", "toskar-"+randomID()[:8]+"-"+name)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	_ = w.WriteField("overwrite", "true")
	if err := w.Close(); err != nil {
		return "", err
	}
	resp, err := c.do(ctx, http.MethodPost, "/upload/image", w.FormDataContentType(), &body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Name      string `json:"name"`
		Subfolder string `json:"subfolder"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.Name == "" {
		return "", fmt.Errorf("ComfyUI didn't keep the picture: %v", err)
	}
	if out.Subfolder != "" {
		return out.Subfolder + "/" + out.Name, nil
	}
	return out.Name, nil
}

func randomID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Run queues a workflow in ComfyUI's API format, waits for it, and returns
// the files it made.
func (c *Client) Run(ctx context.Context, graph map[string]any) ([]Output, error) {
	body, err := json.Marshal(map[string]any{"prompt": graph, "client_id": "toskar-" + randomID()})
	if err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, http.MethodPost, "/prompt", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	var queued struct {
		PromptID   string         `json:"prompt_id"`
		NodeErrors map[string]any `json:"node_errors"`
	}
	err = json.NewDecoder(resp.Body).Decode(&queued)
	resp.Body.Close()
	if err != nil || queued.PromptID == "" {
		return nil, fmt.Errorf("ComfyUI didn't queue the workflow: %v", err)
	}
	if len(queued.NodeErrors) > 0 {
		return nil, fmt.Errorf("ComfyUI refused the workflow: %v", queued.NodeErrors)
	}
	poll := c.Poll
	if poll <= 0 {
		poll = time.Second
	}
	for {
		var history map[string]struct {
			Status struct {
				Completed bool   `json:"completed"`
				Status    string `json:"status_str"`
				Messages  []any  `json:"messages"`
			} `json:"status"`
			Outputs map[string]map[string]json.RawMessage `json:"outputs"`
		}
		if err := c.getJSON(ctx, "/history/"+url.PathEscape(queued.PromptID), &history); err != nil {
			if ctx.Err() != nil {
				c.interrupt(queued.PromptID)
				return nil, ctx.Err()
			}
			return nil, err
		}
		if h, ok := history[queued.PromptID]; ok {
			if h.Status.Status == "error" {
				return nil, fmt.Errorf("the workflow failed in ComfyUI: %v", lastMessage(h.Status.Messages))
			}
			if h.Status.Completed || len(h.Outputs) > 0 {
				return c.collect(ctx, h.Outputs)
			}
		}
		select {
		case <-ctx.Done():
			// Stop it in ComfyUI too, so it doesn't keep the graphics busy.
			c.interrupt(queued.PromptID)
			return nil, ctx.Err()
		case <-time.After(poll):
		}
	}
}

func lastMessage(messages []any) any {
	if len(messages) == 0 {
		return "no reason given"
	}
	return messages[len(messages)-1]
}

// interrupt stops a workflow that is running and takes it off the queue
// if it hasn't started.
func (c *Client) interrupt(promptID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for path, body := range map[string]any{"/interrupt": map[string]any{"prompt_id": promptID}, "/queue": map[string]any{"delete": []string{promptID}}} {
		data, _ := json.Marshal(body)
		if resp, err := c.do(ctx, http.MethodPost, path, "application/json", bytes.NewReader(data)); err == nil {
			resp.Body.Close()
		}
	}
}

// collect downloads the pictures and clips the output nodes saved, in the
// order of their node IDs. Previews (type temp) are left out.
func (c *Client) collect(ctx context.Context, outputs map[string]map[string]json.RawMessage) ([]Output, error) {
	ids := make([]string, 0, len(outputs))
	for id := range outputs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var files []Output
	for _, id := range ids {
		for _, key := range []string{"images", "gifs", "videos"} {
			var list []struct {
				Filename  string `json:"filename"`
				Subfolder string `json:"subfolder"`
				Type      string `json:"type"`
			}
			if raw, ok := outputs[id][key]; !ok || json.Unmarshal(raw, &list) != nil {
				continue
			}
			for _, f := range list {
				if f.Type == "temp" || f.Filename == "" {
					continue
				}
				q := url.Values{"filename": {f.Filename}, "subfolder": {f.Subfolder}, "type": {f.Type}}
				resp, err := c.do(ctx, http.MethodGet, "/view?"+q.Encode(), "", nil)
				if err != nil {
					return nil, err
				}
				data, err := io.ReadAll(io.LimitReader(resp.Body, 512<<20))
				resp.Body.Close()
				if err != nil {
					return nil, err
				}
				files = append(files, Output{Name: f.Filename, Data: data})
			}
		}
	}
	if len(files) == 0 {
		return nil, errors.New("the workflow finished without saving a picture or clip; it needs a Save Image or Save Video node")
	}
	return files, nil
}

// Picture is a picture from the chat for a workflow's Load Image node.
type Picture struct {
	Name string
	Data []byte
}

// RunWorkflow uploads the picture if there is one, fills the workflow's
// inputs, and runs it. It returns the files and the seed it used.
func (c *Client) RunWorkflow(ctx context.Context, w Workflow, v Values, pic *Picture) ([]Output, int64, error) {
	if pic != nil {
		if w.Inputs.Image == nil {
			return nil, 0, fmt.Errorf("the %s workflow doesn't take a picture", w.Name)
		}
		name, err := c.upload(ctx, pic.Name, pic.Data)
		if err != nil {
			return nil, 0, err
		}
		v.Image = name
	}
	graph, seed, err := w.Fill(v)
	if err != nil {
		return nil, 0, err
	}
	files, err := c.Run(ctx, graph)
	return files, seed, err
}
