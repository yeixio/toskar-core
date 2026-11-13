//go:build unix

package llamacpp

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Agents that ask for the same model at once share one llama-server, which
// keeps its slots until a start asks for others (#1135).
func TestStartsShareOneServer(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "llamacpp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(os.Args[0], filepath.Join(root, "llamacpp", "llama-server")); err != nil {
		t.Fatal(err)
	}
	if bundledLlamaServer() != "" {
		t.Skip("a bundled llama-server takes precedence on this machine")
	}
	t.Setenv(fakeLlamaEnv, filepath.Join(root, "child.pid"))
	model := filepath.Join(root, "model.gguf")
	_ = os.WriteFile(model, []byte("GGUF"), 0o600)
	r := New(root, t.TempDir())
	t.Cleanup(func() {
		list, _ := r.ListRunning(context.Background())
		for _, m := range list {
			_ = r.StopModel(context.Background(), m.ID)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg := pluginapi.ModelStartConfig{ModelID: "qwen", ModelPath: model, Context: 8192, Slots: 2}

	ids := make([]string, 3)
	var wg sync.WaitGroup
	for i := range ids {
		wg.Go(func() {
			m, err := r.StartModel(ctx, cfg)
			if err != nil {
				t.Errorf("start %d: %v", i, err)
				return
			}
			ids[i] = m.ID
		})
	}
	wg.Wait()
	if ids[0] == "" || ids[0] != ids[1] || ids[1] != ids[2] {
		t.Fatalf("three starts at once: %v", ids)
	}
	list, _ := r.ListRunning(ctx)
	if len(list) != 1 || list[0].Slots != 2 || list[0].Context != 8192 {
		t.Fatalf("running: %+v", list)
	}
	if m, err := r.StartModel(ctx, pluginapi.ModelStartConfig{ModelID: "qwen", ModelPath: model}); err != nil || m.ID != ids[0] {
		t.Fatalf("a start that asks for no slots: %v, %s", err, m.ID)
	}
	one, err := r.StartModel(ctx, pluginapi.ModelStartConfig{ModelID: "qwen", ModelPath: model, Context: 8192, Slots: 1})
	if err != nil || one.ID == ids[0] || one.Slots != 0 && one.Slots != 1 {
		t.Fatalf("one slot: %v, %+v", err, one)
	}
	if list, _ := r.ListRunning(ctx); len(list) != 1 || list[0].Slots != 1 {
		t.Fatalf("after the change: %+v", list)
	}
}
