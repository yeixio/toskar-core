package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/yggdrasil-core/internal/artifacts"
	"github.com/yeixio/yggdrasil-core/internal/mimir"
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

func TestSmallModelNote(t *testing.T) {
	tiny := installed("llama-1b", "Llama 3.2 1B", 1.6e9)
	tiny.Parameters = "1B"
	big := installed("qwen-7b", "Qwen 2.5 7B", 6.4e9)
	big.Parameters = "7B"

	note := smallModelNote("file", "llama-1b", []contracts.Model{tiny, big}, 24e9, false)
	if !strings.Contains(note, "Llama 3.2 1B is a small model") || !strings.Contains(note, "from your files") || !strings.Contains(note, "choose Qwen 2.5 7B or Auto") {
		t.Fatalf("note = %q", note)
	}
	// With nothing larger installed, point to the Models page.
	if note := smallModelNote("knowledge", "llama-1b", []contracts.Model{tiny}, 24e9, false); !strings.Contains(note, "your knowledge") || !strings.Contains(note, "Models page") {
		t.Fatalf("note = %q", note)
	}
	// No data, or a model that is not small: no note.
	if smallModelNote("", "llama-1b", []contracts.Model{tiny}, 24e9, false) != "" || smallModelNote("file", "qwen-7b", []contracts.Model{tiny, big}, 24e9, false) != "" {
		t.Fatal("unexpected note")
	}
}

func TestTraceKnowsWhenDataWasUsed(t *testing.T) {
	tr := &turnTrace{}
	if tr.dataKind() != "" {
		t.Fatal("nothing used yet")
	}
	tr.knowledge([]mimir.Hit{{Title: "row 1", SourceName: "inventory.csv"}})
	if tr.dataKind() != "knowledge" {
		t.Fatal("knowledge")
	}
	tr.attachment(artifacts.Artifact{Name: "tires.xlsx", Producer: artifacts.ProducerAssistant}, 1, 1)
	if tr.dataKind() != "file" {
		t.Fatal("files outrank knowledge in the note")
	}
	tr.noticeIfNone("first")
	tr.noticeIfNone("second")
	if tr.meta().Notice != "first" {
		t.Fatal("an existing notice is kept")
	}
}

func TestTraceRecordsPlansAndChecks(t *testing.T) {
	tr := &turnTrace{}
	tr.planned(3, true)
	tr.verified(2, 1, "20")
	meta := tr.meta()
	if meta.Steps[0].Text != "Split the request into 3 parts and looked them up side by side" ||
		meta.Steps[1].Text != "Checked the figures; some could not be confirmed" ||
		meta.Notice != "Yggdrasil could not confirm 20 in the sources. Check before relying on it." {
		t.Fatalf("meta = %+v", meta)
	}
	tr = &turnTrace{}
	tr.verified(1, 1, "")
	tr.planned(2, false)
	if meta := tr.meta(); meta.Steps[0].Text != "Checked the figures and corrected 1 figure" || meta.Steps[1].Text != "Worked through the request in 2 parts" || meta.Notice != "" {
		t.Fatalf("meta = %+v", meta)
	}
}
