package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/api"
	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/comfyui"
	"github.com/yeixio/toskar-core/internal/egress"
)

// fakeComfy runs any workflow at once and saves one picture.
func fakeComfy(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /upload/image", func(w http.ResponseWriter, r *http.Request) {
		_, head, _ := r.FormFile("image")
		_ = json.NewEncoder(w).Encode(map[string]string{"name": head.Filename})
	})
	mux.HandleFunc("POST /prompt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"prompt_id":"p1"}`)
	})
	mux.HandleFunc("GET /history/p1", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"p1":{"status":{"status_str":"success","completed":true},"outputs":{"9":{"images":[{"filename":"ComfyUI_00007_.png","subfolder":"","type":"output"}]}}}}`)
	})
	mux.HandleFunc("GET /view", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "\x89PNG\r\n\x1a\nfake")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func comfyApp(t *testing.T, base string) (*App, context.Context) {
	t.Helper()
	a, conv := memoryApp(t)
	a.Artifacts = artifacts.NewStore(a.DB.SQL, t.TempDir())
	a.Egress = egress.New(a.DB.SQL)
	a.Comfy = &comfyui.Client{Poll: time.Millisecond}
	a.Comfy.SetConfig(comfyui.Config{BaseURL: base})
	a.ComfyFlows = &comfyui.Store{Path: filepath.Join(t.TempDir(), "workflows.json")}
	return a, artifacts.WithConversation(context.Background(), conv)
}

func readFixture(t *testing.T, name string) json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "comfyui", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestComfyToolRunsASavedWorkflow(t *testing.T) {
	srv := fakeComfy(t)
	a, ctx := comfyApp(t, srv.URL)
	tool := &comfyTool{app: a}
	if ok, why := tool.Available(); ok || !strings.Contains(why, "No ComfyUI workflow") {
		t.Fatalf("available without workflows: %v %q", ok, why)
	}
	// Saved with detected inputs, under a clean name.
	saved, err := a.SaveComfyWorkflow(ctx, api.ComfyWorkflowInput{Name: "Watercolor Pictures", Graph: readFixture(t, "txt2img.json")})
	if err != nil || saved.Name != "watercolor-pictures" || saved.Inputs.Prompt == nil || saved.Nodes != 7 {
		t.Fatalf("saved = %+v, %v", saved, err)
	}
	if _, err := a.SaveComfyWorkflow(ctx, api.ComfyWorkflowInput{Name: "upscale", Graph: readFixture(t, "upscale.json")}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := tool.Available(); !ok {
		t.Fatal("not available")
	}
	// Two workflows: one must be named, and the error lists them.
	if _, err := tool.Execute(ctx, map[string]any{"prompt": "a fox"}); err == nil || !strings.Contains(err.Error(), "watercolor-pictures") || !strings.Contains(err.Error(), "upscale") {
		t.Fatalf("unnamed: %v", err)
	}
	out, err := tool.Execute(ctx, map[string]any{"workflow": "watercolor-pictures", "prompt": "a fox", "seed": float64(9)})
	if err != nil {
		t.Fatal(err)
	}
	files, _ := out["files"].([]map[string]any)
	if len(files) != 1 || files[0]["name"] != "watercolor-pictures-00007.png" || out["seed"] != int64(9) {
		t.Fatalf("out = %+v", out)
	}
	list, _ := a.Artifacts.List(ctx, artifacts.ConversationFrom(ctx))
	if len(list) != 1 || list[0].Details["workflow"] != "watercolor-pictures" {
		t.Fatalf("artifacts = %+v", list)
	}
	// The upscale takes the picture it just made.
	if _, err := tool.Execute(ctx, map[string]any{"workflow": "upscale", "file": "watercolor-pictures-00007.png"}); err != nil {
		t.Fatal(err)
	}
	// A ComfyUI on this computer: nothing left it.
	if got, _ := a.Egress.List(context.Background(), egress.Filter{}); len(got) != 0 {
		t.Fatalf("egress = %+v", got)
	}
}

func TestComfyName(t *testing.T) {
	for in, want := range map[string]string{"ComfyUI_00001_.png": "up-00001.png", "clip.webm": "up.webm", "x_12.MP4": "up-12.mp4"} {
		if got := comfyName("up", in); got != want {
			t.Errorf("comfyName(%q) = %q, want %q", in, got, want)
		}
	}
	if !localHost("127.0.0.1:8188") || !localHost("localhost:8188") || localHost("192.168.1.20:8188") {
		t.Error("localHost")
	}
}

func TestComfyUIStatusSaysWhyACheckFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "no", http.StatusForbidden) }))
	defer srv.Close()
	a, ctx := comfyApp(t, srv.URL)
	st, err := a.ComfyUI(ctx)
	if err != nil || st.Info != nil || !strings.Contains(st.Error, "refused the token") {
		t.Fatalf("status = %+v, %v", st, err)
	}
	if _, err := a.SaveComfyWorkflow(ctx, api.ComfyWorkflowInput{Name: "x", Graph: json.RawMessage(`{"1":{"class_type":"CheckpointLoaderSimple","inputs":{}}}`)}); err == nil {
		t.Fatal("saved a workflow with no prompt or picture")
	}
}
