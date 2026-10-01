package app

import (
	"context"
	"strings"
	"testing"
	"time"

	modelhealth "github.com/yeixio/yggdrasil-core/internal/models/health"
	"github.com/yeixio/yggdrasil-core/pkg/contracts"
)

func installed(id, name string, mem uint64) contracts.Model {
	return contracts.Model{ID: id, DisplayName: name, Installed: true, MemoryNeeded: mem,
		Tags: []string{"general"}, Purpose: []string{"general"}, Capabilities: contracts.ModelCapabilities{ToolCalling: true}}
}

func TestFallbackExplainsAndWarnsWhenSmaller(t *testing.T) {
	models := []contracts.Model{installed("big", "Qwen 14B", 12e9), installed("small", "Llama 1B", 1.6e9)}
	oom := modelhealth.Encode(modelhealth.Failure{Kind: "model_health", Reason: "runtime_error", LikelyMemoryPressure: true})
	next, step, notice, ok := fallbackFrom("big", oom, models, 24e9)
	if !ok || next.ID != "small" {
		t.Fatalf("next = %+v %v", next, ok)
	}
	if step != "Qwen 14B ran out of memory, so Llama 1B answered instead" {
		t.Fatalf("step = %q", step)
	}
	if !strings.Contains(notice, "smaller Llama 1B") || !strings.Contains(notice, "less detailed") {
		t.Fatalf("notice = %q", notice)
	}
	// A model of the same size answers as well, so there is no notice.
	models = append(models, installed("peer", "Mistral 7B", 11e9))
	if _, _, notice, _ := fallbackFrom("big", "llama-server exited", models, 24e9); notice != "" {
		t.Fatalf("notice for a peer = %q", notice)
	}
	if _, _, _, ok := fallbackFrom("big", "x", models[:1], 24e9); ok {
		t.Fatal("nothing to fall back to")
	}
}

func TestRecoverableOnlyBeforeAnythingHappened(t *testing.T) {
	ctx := context.Background()
	env := &chatExecEnv{trace: &turnTrace{}}
	if !recoverable(ctx, "llama-server exited", "", env) {
		t.Fatal("a failure before any output should be retried")
	}
	if recoverable(ctx, "llama-server exited", "partial answer", env) {
		t.Fatal("a failure after output must not be retried")
	}
	if recoverable(ctx, "context canceled", "", env) {
		t.Fatal("a stopped turn must not be retried")
	}
	stopped, cancel := context.WithCancel(ctx)
	cancel()
	if recoverable(stopped, "llama-server exited", "", env) {
		t.Fatal("a cancelled turn must not be retried")
	}
	env.trace.tool("filesystem.write", map[string]any{"path": "notes.txt"}, nil)
	if recoverable(ctx, "llama-server exited", "", env) {
		t.Fatal("a turn that changed a file must not be repeated")
	}
}

func TestTraceCarriesRouteAndNotice(t *testing.T) {
	tr := &turnTrace{}
	tr.routed("Auto chose Qwen 7B for a coding question")
	tr.recovered("Qwen 7B stopped responding, so Llama 1B answered instead", "Qwen 7B could not answer…")
	meta := tr.meta()
	if meta == nil || len(meta.Steps) != 2 || meta.Steps[0].Kind != "route" || meta.Steps[1].Kind != "recover" || meta.Notice == "" {
		t.Fatalf("meta = %+v", meta)
	}
}

func TestAutoAvoidsAModelThatJustFailed(t *testing.T) {
	a := &App{}
	if a.recentlyFailed("big") {
		t.Fatal("nothing has failed yet")
	}
	a.noteModelFailed("big")
	if !a.recentlyFailed("big") || a.recentlyFailed("small") {
		t.Fatal("only the failed model is avoided")
	}
	a.failedModels.Store("big", time.Now().Add(-failedFor-time.Second))
	if a.recentlyFailed("big") {
		t.Fatal("a model is tried again after a while")
	}
}
