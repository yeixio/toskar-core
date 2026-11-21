package comfyui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// fake is a ComfyUI that runs every workflow in two polls and saves one
// picture, or fails it, or never finishes.
type fake struct {
	mu          sync.Mutex
	token       string
	queued      map[string]any
	uploads     []string
	polls       int
	fail        bool
	hang        bool
	interrupted bool
}

func (f *fake) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /system_stats", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"system":{"comfyui_version":"0.3.40"},"devices":[{"name":"cuda:0 NVIDIA GeForce RTX 4090","type":"cuda","vram_total":25393692672,"vram_free":24000000000}]}`)
	})
	mux.HandleFunc("GET /object_info", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"CheckpointLoaderSimple":{"input":{"required":{"ckpt_name":[["sdxl.safetensors","v1-5.safetensors"],{}]}}},
			"UpscaleModelLoader":{"input":{"required":{"model_name":[["4x-UltraSharp.pth"]]}}},"KSampler":{"input":{"required":{}}}}`)
	})
	mux.HandleFunc("POST /upload/image", func(w http.ResponseWriter, r *http.Request) {
		_, head, err := r.FormFile("image")
		if err != nil {
			t.Errorf("upload: %v", err)
			return
		}
		f.mu.Lock()
		f.uploads = append(f.uploads, head.Filename)
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]string{"name": head.Filename, "subfolder": "", "type": "input"})
	})
	mux.HandleFunc("POST /prompt", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Prompt   map[string]any `json:"prompt"`
			ClientID string         `json:"client_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ClientID == "" {
			t.Errorf("prompt body: %v", err)
		}
		f.mu.Lock()
		f.queued = body.Prompt
		f.mu.Unlock()
		_, _ = io.WriteString(w, `{"prompt_id":"p1","number":1,"node_errors":{}}`)
	})
	mux.HandleFunc("GET /history/p1", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.polls++
		switch {
		case f.hang || f.polls < 2:
			_, _ = io.WriteString(w, `{}`)
		case f.fail:
			_, _ = io.WriteString(w, `{"p1":{"status":{"status_str":"error","completed":false,"messages":[["execution_error",{"exception_message":"CUDA out of memory"}]]},"outputs":{}}}`)
		default:
			_, _ = io.WriteString(w, `{"p1":{"status":{"status_str":"success","completed":true},"outputs":{
				"9":{"images":[{"filename":"ComfyUI_00001_.png","subfolder":"","type":"output"}]},
				"10":{"images":[{"filename":"preview.png","subfolder":"","type":"temp"}]}}}}`)
		}
	})
	mux.HandleFunc("GET /view", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("filename") != "ComfyUI_00001_.png" || r.URL.Query().Get("type") != "output" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, "\x89PNG fake")
	})
	mux.HandleFunc("POST /interrupt", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		f.interrupted = true
		f.mu.Unlock()
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.token != "" && r.Header.Get("Authorization") != "Bearer "+f.token {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func setup(t *testing.T, f *fake, token string) *Client {
	t.Helper()
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	c := &Client{Poll: time.Millisecond}
	c.SetConfig(Config{BaseURL: srv.URL, Token: token})
	return c
}

func TestNormalizeURL(t *testing.T) {
	for raw, want := range map[string]string{"": "", " http://127.0.0.1:8188/ ": "http://127.0.0.1:8188", "https://comfy.lan": "https://comfy.lan"} {
		if got, err := NormalizeURL(raw); err != nil || got != want {
			t.Errorf("NormalizeURL(%q) = %q, %v", raw, got, err)
		}
	}
	for _, raw := range []string{"127.0.0.1:8188", "ftp://x", "http://x/?a=1"} {
		if _, err := NormalizeURL(raw); err == nil {
			t.Errorf("NormalizeURL(%q) took it", raw)
		}
	}
}

func TestCheck(t *testing.T) {
	c := setup(t, &fake{token: "secret"}, "secret")
	info, err := c.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "0.3.40" || len(info.Devices) != 1 || info.CustomNodes != 3 {
		t.Fatalf("info = %+v", info)
	}
	if got := strings.Join(info.Models["checkpoints"], ","); got != "sdxl.safetensors,v1-5.safetensors" {
		t.Errorf("checkpoints = %q", got)
	}
	if len(info.Models["upscalers"]) != 1 || info.Models["loras"] != nil {
		t.Errorf("models = %v", info.Models)
	}
}

func TestCheckWrongToken(t *testing.T) {
	c := setup(t, &fake{token: "secret"}, "wrong")
	if _, err := c.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "refused the token") {
		t.Fatalf("err = %v", err)
	}
}

func TestNotSetUp(t *testing.T) {
	if _, err := (&Client{}).Check(context.Background()); !errors.Is(err, ErrNotSetUp) {
		t.Fatalf("err = %v", err)
	}
}

func TestDetect(t *testing.T) {
	in, err := Detect(fixture(t, "txt2img.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := Inputs{Prompt: &Input{"6", "text"}, Negative: &Input{"7", "text"}, Seed: &Input{"3", "seed"}, Width: &Input{"5", "width"}, Height: &Input{"5", "height"}}
	got, _ := json.Marshal(in)
	exp, _ := json.Marshal(want)
	if string(got) != string(exp) {
		t.Fatalf("Detect = %s, want %s", got, exp)
	}
	in, err = Detect(fixture(t, "upscale.json"))
	if err != nil || in.Image == nil || in.Image.Node != "1" || in.Prompt != nil || in.Seed != nil {
		t.Fatalf("upscale Detect = %+v, %v", in, err)
	}
}

func TestDetectEditorFormat(t *testing.T) {
	if _, err := Detect(json.RawMessage(`{"nodes":[],"links":[]}`)); err == nil || !strings.Contains(err.Error(), "Export (API)") {
		t.Fatalf("err = %v", err)
	}
}

func TestWorkflowCheck(t *testing.T) {
	w := Workflow{Name: "pictures", Graph: fixture(t, "txt2img.json"), Inputs: Inputs{Prompt: &Input{"6", "text"}}}
	if err := w.Check(); err != nil {
		t.Fatal(err)
	}
	w.Inputs.Seed = &Input{"6", "seed"}
	if err := w.Check(); err == nil || !strings.Contains(err.Error(), "seed") {
		t.Fatalf("bad field: %v", err)
	}
	w.Inputs.Seed = nil
	w.Name = "Has Spaces"
	if err := w.Check(); err == nil {
		t.Fatal("bad name passed")
	}
	if CleanName(" Remove background! ") != "remove-background" {
		t.Errorf("CleanName = %q", CleanName(" Remove background! "))
	}
}

func TestRunWorkflow(t *testing.T) {
	f := &fake{}
	c := setup(t, f, "")
	graph := fixture(t, "txt2img.json")
	in, _ := Detect(graph)
	w := Workflow{Name: "pictures", Graph: graph, Inputs: in}
	files, seed, err := c.RunWorkflow(context.Background(), w, Values{Prompt: "a red fox", Seed: 42, Width: 700, Height: 400}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if seed != 42 || len(files) != 1 || files[0].Name != "ComfyUI_00001_.png" || string(files[0].Data) != "\x89PNG fake" {
		t.Fatalf("seed %d files %+v", seed, files)
	}
	inputs := func(id string) map[string]any { return f.queued[id].(map[string]any)["inputs"].(map[string]any) }
	if inputs("6")["text"] != "a red fox" || inputs("7")["text"] != "text, watermark" || inputs("3")["seed"] != float64(42) {
		t.Errorf("queued = %v", f.queued)
	}
	if inputs("5")["width"] != float64(704) || inputs("5")["height"] != float64(400) {
		t.Errorf("size = %v x %v", inputs("5")["width"], inputs("5")["height"])
	}
	// No seed picks a new one.
	_, seed, err = c.RunWorkflow(context.Background(), w, Values{Prompt: "a fox"}, nil)
	if err != nil || seed <= 0 {
		t.Fatalf("new seed %d, %v", seed, err)
	}
}

func TestRunWorkflowUploadsPicture(t *testing.T) {
	f := &fake{}
	c := setup(t, f, "")
	graph := fixture(t, "upscale.json")
	in, _ := Detect(graph)
	w := Workflow{Name: "upscale", Graph: graph, Inputs: in}
	if _, _, err := c.RunWorkflow(context.Background(), w, Values{}, nil); err == nil || !strings.Contains(err.Error(), "needs a picture") {
		t.Fatalf("no picture: %v", err)
	}
	if _, _, err := c.RunWorkflow(context.Background(), w, Values{Prompt: "x"}, &Picture{Name: "cat.png", Data: []byte("png")}); err == nil || !strings.Contains(err.Error(), "prompt") {
		t.Fatalf("prompt to a workflow without one: %v", err)
	}
	if _, _, err := c.RunWorkflow(context.Background(), w, Values{}, &Picture{Name: "cat.png", Data: []byte("png")}); err != nil {
		t.Fatal(err)
	}
	if len(f.uploads) != 2 || !strings.HasSuffix(f.uploads[1], "-cat.png") {
		t.Fatalf("uploads = %v", f.uploads)
	}
	if got := f.queued["1"].(map[string]any)["inputs"].(map[string]any)["image"]; got != f.uploads[1] {
		t.Errorf("image = %v, want %s", got, f.uploads[1])
	}
}

func TestRunFails(t *testing.T) {
	c := setup(t, &fake{fail: true}, "")
	if _, err := c.Run(context.Background(), map[string]any{}); err == nil || !strings.Contains(err.Error(), "CUDA out of memory") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunCancelInterrupts(t *testing.T) {
	f := &fake{hang: true}
	c := setup(t, f, "")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := c.Run(ctx, map[string]any{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.interrupted {
		t.Error("ComfyUI wasn't told to stop")
	}
}

func TestStore(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "comfyui", "workflows.json")}
	if list, err := s.List(); err != nil || len(list) != 0 {
		t.Fatalf("empty: %v %v", list, err)
	}
	up := Workflow{Name: "upscale", Graph: fixture(t, "upscale.json"), Inputs: Inputs{Image: &Input{"1", "image"}}}
	pics := Workflow{Name: "pictures", Graph: fixture(t, "txt2img.json"), Inputs: Inputs{Prompt: &Input{"6", "text"}}}
	for _, w := range []Workflow{up, pics, pics} {
		if err := s.Save(w); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Save(Workflow{Name: "bad", Graph: json.RawMessage(`{"nodes":[],"links":[]}`)}); err == nil {
		t.Fatal("saved the editor's format")
	}
	list, _ := s.List()
	if len(list) != 2 || list[0].Name != "pictures" || list[1].Name != "upscale" {
		t.Fatalf("list = %v", list)
	}
	if w, ok, _ := s.Get("upscale"); !ok || w.Inputs.Image == nil {
		t.Fatalf("Get = %+v %v", w, ok)
	}
	if err := s.Delete("upscale"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Get("upscale"); ok {
		t.Fatal("still there")
	}
}
