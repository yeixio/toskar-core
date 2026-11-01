package llamacpp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

func TestStartArgsForSupportingModes(t *testing.T) {
	chat := startArgs(pluginapi.ModelStartConfig{ModelPath: "m.gguf"}, 9000)
	if slices.Contains(chat, "--embedding") || !slices.Contains(chat, "8192") {
		t.Fatalf("chat args: %v", chat)
	}
	// One slot, so every request has the window it's told it has (#834).
	if !strings.Contains(strings.Join(chat, " "), "--parallel 1") {
		t.Fatalf("chat args: %v", chat)
	}
	emb := strings.Join(startArgs(pluginapi.ModelStartConfig{ModelPath: "e.gguf", Mode: pluginapi.ModeEmbedding, Context: 8192}, 9000), " ")
	for _, want := range []string{"--embedding", "--ctx-size 2048", "--ubatch-size 2048"} {
		if !strings.Contains(emb, want) {
			t.Fatalf("embedding args %q lack %q", emb, want)
		}
	}
	rr := startArgs(pluginapi.ModelStartConfig{ModelPath: "r.gguf", Mode: pluginapi.ModeReranking}, 9000)
	if !slices.Contains(rr, "--reranking") || slices.Contains(rr, "--embedding") {
		t.Fatalf("reranking args: %v", rr)
	}
}

func TestEmbedKeepsInputOrder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		// Answer out of order, as a server with several slots may.
		data := []map[string]any{}
		for i := len(body.Input) - 1; i >= 0; i-- {
			data = append(data, map[string]any{"index": i, "embedding": []float32{float32(len(body.Input[i])), 1}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer srv.Close()

	vecs, err := Embed(context.Background(), srv.URL, []string{"a", "bbb"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 || vecs[0][0] != 1 || vecs[1][0] != 3 {
		t.Fatalf("got %v", vecs)
	}
}

func TestRerankScoresInDocumentOrder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/rerank" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{
			{"index": 1, "relevance_score": 4.5},
			{"index": 0, "relevance_score": -2.0},
		}})
	}))
	defer srv.Close()

	scores, err := Rerank(context.Background(), srv.URL, "q", []string{"x", "y"})
	if err != nil {
		t.Fatal(err)
	}
	if scores[0] != -2 || scores[1] != 4.5 {
		t.Fatalf("got %v", scores)
	}
}

func TestEmbedReportsServerErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "pooling type none is not OAI compatible", http.StatusBadRequest)
	}))
	defer srv.Close()
	if _, err := Embed(context.Background(), srv.URL, []string{"a"}); err == nil || !strings.Contains(err.Error(), "pooling") {
		t.Fatalf("got %v", err)
	}
}

func TestToolIsFoundBesideLlamaServer(t *testing.T) {
	dir := t.TempDir()
	rt := New(dir, t.TempDir())
	if _, err := rt.Tool("llama-export-lora"); err == nil || !strings.Contains(err.Error(), "no llama-export-lora") {
		t.Fatalf("got %v", err)
	}
	name := "llama-export-lora"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.MkdirAll(filepath.Join(dir, "llamacpp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "llamacpp", name), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if bundledLlamaServer() != "" {
		t.Skip("a bundled llama-server takes precedence")
	}
	path, err := rt.Tool("llama-export-lora")
	if err != nil || filepath.Base(path) != name {
		t.Fatalf("got %q, %v", path, err)
	}
}

// A vision model starts with its projector, so it can see pictures (#191).
func TestStartArgsWithProjector(t *testing.T) {
	args := strings.Join(startArgs(pluginapi.ModelStartConfig{ModelPath: "v.gguf", Projector: "v.mmproj.gguf"}, 9000), " ")
	if !strings.Contains(args, "--mmproj v.mmproj.gguf") {
		t.Fatalf("args = %s", args)
	}
	if strings.Contains(strings.Join(startArgs(pluginapi.ModelStartConfig{ModelPath: "m.gguf"}, 9000), " "), "--mmproj") {
		t.Fatal("a text model was given a projector")
	}
}

// A chat model starts with the window it's given (#962), and a running one
// with another window is started again; a start that asks for none takes
// it as it runs.
func TestChatContextWindow(t *testing.T) {
	args := strings.Join(startArgs(pluginapi.ModelStartConfig{ModelPath: "m.gguf", Context: 32768}, 9000), " ")
	if !strings.Contains(args, "--ctx-size 32768 --parallel 1") {
		t.Errorf("args %q", args)
	}
	running := pluginapi.RunningModel{Context: 32768}
	for _, c := range []struct {
		cfg  pluginapi.ModelStartConfig
		same bool
	}{
		{pluginapi.ModelStartConfig{Context: 32768}, true},
		{pluginapi.ModelStartConfig{Context: 16384}, false},
		{pluginapi.ModelStartConfig{}, true},
		{pluginapi.ModelStartConfig{Context: 8192, Mode: pluginapi.ModeEmbedding}, true},
	} {
		if got := sameContext(running, c.cfg); got != c.same {
			t.Errorf("%+v: same %v", c.cfg, got)
		}
	}
	if !sameContext(pluginapi.RunningModel{Context: 8192}, pluginapi.ModelStartConfig{Context: 8192}) {
		t.Error("8K running, 8K asked")
	}
	if chatContext(pluginapi.ModelStartConfig{}) != 8192 || chatContext(pluginapi.ModelStartConfig{Mode: pluginapi.ModeReranking}) != 0 {
		t.Error("chat context defaults")
	}
}
