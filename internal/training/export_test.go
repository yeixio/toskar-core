package training

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// writeTestAdapter writes the header of a GGUF LoRA adapter for one 64×128
// tensor at rank 8, with metadata of several types to skip.
func writeTestAdapter(t *testing.T, path string) {
	t.Helper()
	var b bytes.Buffer
	le := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	str := func(s string) { le(uint64(len(s))); b.WriteString(s) }
	b.WriteString("GGUF")
	le(uint32(3))
	le(uint64(2)) // tensors
	le(uint64(3)) // metadata
	str("general.architecture")
	le(uint32(ggufString))
	str("llama")
	str("adapter.lora.alpha")
	le(uint32(ggufF32))
	le(float32(16))
	str("tokenizer.ggml.tokens")
	le(uint32(ggufArray))
	le(uint32(ggufString))
	le(uint64(2))
	str("<s>")
	str("hello")
	for _, tensor := range []struct {
		name string
		dims []uint64
	}{{"blk.0.attn_q.weight.lora_a", []uint64{64, 8}}, {"blk.0.attn_q.weight.lora_b", []uint64{8, 128}}} {
		str(tensor.name)
		le(uint32(len(tensor.dims)))
		for _, d := range tensor.dims {
			le(d)
		}
		le(uint32(0)) // F32
		le(uint64(0))
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGGUFHeaderGivesMergedGrowth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "adapter.gguf")
	writeTestAdapter(t, path)
	tensors, err := readGGUFTensors(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(tensors) != 2 || tensors[1].Name != "blk.0.attn_q.weight.lora_b" || tensors[1].Dims[1] != 128 {
		t.Fatalf("tensors = %+v", tensors)
	}
	// The merged q tensor is 64×128 weights at two bytes each.
	if got := mergedGrowth(tensors); got != 64*128*2 {
		t.Fatalf("growth = %d", got)
	}
	if err := os.WriteFile(path, []byte("gguf"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readGGUFTensors(path); err == nil {
		t.Fatal("a file that is not GGUF must be refused")
	}
}

// fakeExportTool is a shell script standing in for llama-export-lora: it
// writes the base model's bytes and "+lora" to the -o file. With FAIL set it
// fails the way llama-export-lora does; with SLOW set it waits to be stopped.
const fakeExportTool = `#!/bin/sh
while [ $# -gt 0 ]; do
  case "$1" in
    -m) base="$2"; shift 2 ;;
    --lora) shift 2 ;;
    -o) out="$2"; shift 2 ;;
    *) shift ;;
  esac
done
if [ -f "$(dirname "$0")/FAIL" ]; then echo "loading base"; echo "error: adapter does not match the model" >&2; exit 1; fi
if [ -f "$(dirname "$0")/SLOW" ]; then printf partial > "$out"; sleep 30; fi
cat "$base" > "$out"
printf '+lora' >> "$out"
`

type exportHarness struct {
	*harness
	ai     SpecializedAI
	tool   string
	events chan string
}

func newExportHarness(t *testing.T) *exportHarness {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake llama-export-lora is a shell script")
	}
	h := newHarness(t, "ok")
	ctx := context.Background()
	ai := readyAI(t, h)
	job, err := h.svc.StartTraining(ctx, ai.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, h, job.ID)
	rev, err := h.svc.d.Repo.GetRevision(ctx, ai.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	writeTestAdapter(t, rev.adapterPath)

	toolDir := t.TempDir()
	tool := filepath.Join(toolDir, "llama-export-lora")
	if err := os.WriteFile(tool, []byte(fakeExportTool), 0o755); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(t.TempDir(), "small-q4.gguf")
	if err := os.WriteFile(base, []byte("BASE"), 0o644); err != nil {
		t.Fatal(err)
	}
	eh := &exportHarness{harness: h, ai: ai, tool: tool, events: make(chan string, 8)}
	h.svc.d.ExportTool = func(context.Context) (string, error) { return tool, nil }
	h.svc.d.ModelPath = func(_ context.Context, id string) (string, error) {
		if id != "small-q4" {
			return "", errors.New("not installed")
		}
		return base, nil
	}
	h.svc.d.Publish = func(eventType string, _ map[string]any) {
		if strings.HasPrefix(eventType, "training.export.") {
			eh.events <- eventType
		}
	}
	return eh
}

func TestExportMergesARevisionIntoOneGGUF(t *testing.T) {
	h := newExportHarness(t)
	ctx := context.Background()
	if st, err := h.svc.ExportStatus(ctx, h.ai.ID, 1); err != nil || st.State != ExportNone {
		t.Fatalf("before export: %+v, %v", st, err)
	}
	st, err := h.svc.Export(ctx, h.ai.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != ExportExporting && st.State != ExportReady {
		t.Fatalf("state = %s", st.State)
	}
	h.svc.Wait()
	if ev := <-h.events; ev != EventExportCompleted {
		t.Fatalf("event = %s", ev)
	}
	st, err = h.svc.ExportStatus(ctx, h.ai.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != ExportReady || st.Filename != "tire-bot-r1.gguf" || st.SizeBytes != uint64(len("BASE+lora")) ||
		!strings.HasPrefix(st.Instructions, "You are Tire Bot") {
		t.Fatalf("status = %+v", st)
	}
	path, name, err := h.svc.ExportFile(ctx, h.ai.ID, 1)
	if err != nil || name != "tire-bot-r1.gguf" {
		t.Fatalf("file = %s %s %v", path, name, err)
	}
	if b, _ := os.ReadFile(path); string(b) != "BASE+lora" {
		t.Fatalf("content = %q", b)
	}
	// Asking again returns the finished file instead of merging again.
	if st, _ := h.svc.Export(ctx, h.ai.ID, 1); st.State != ExportReady {
		t.Fatalf("second export = %+v", st)
	}

	if err := h.svc.DeleteAI(ctx, h.ai.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("export left behind after the AI was deleted: %v", err)
	}
}

func TestExportReportsFailureAndCanBeRetried(t *testing.T) {
	h := newExportHarness(t)
	ctx := context.Background()
	failFlag := filepath.Join(filepath.Dir(h.tool), "FAIL")
	if err := os.WriteFile(failFlag, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Export(ctx, h.ai.ID, 1); err != nil {
		t.Fatal(err)
	}
	h.svc.Wait()
	if ev := <-h.events; ev != EventExportFailed {
		t.Fatalf("event = %s", ev)
	}
	st, _ := h.svc.ExportStatus(ctx, h.ai.ID, 1)
	if st.State != ExportFailed || !strings.Contains(st.Error, "adapter does not match the model") {
		t.Fatalf("status = %+v", st)
	}
	if _, _, err := h.svc.ExportFile(ctx, h.ai.ID, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a failed export has no file, got %v", err)
	}

	_ = os.Remove(failFlag)
	if _, err := h.svc.Export(ctx, h.ai.ID, 1); err != nil {
		t.Fatal(err)
	}
	h.svc.Wait()
	if st, _ := h.svc.ExportStatus(ctx, h.ai.ID, 1); st.State != ExportReady {
		t.Fatalf("retry = %+v", st)
	}
}

func TestDeletingAnExportInProgressStopsIt(t *testing.T) {
	h := newExportHarness(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(filepath.Dir(h.tool), "SLOW"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Export(ctx, h.ai.ID, 1); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if st, _ := h.svc.ExportStatus(ctx, h.ai.ID, 1); st.State != ExportExporting {
		t.Fatalf("status = %+v", st)
	}
	start := time.Now()
	if err := h.svc.DeleteExport(ctx, h.ai.ID, 1); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); h.svc.Wait() }()
	wg.Wait()
	if time.Since(start) > 10*time.Second {
		t.Fatal("the merge was not stopped")
	}
	st, _ := h.svc.ExportStatus(ctx, h.ai.ID, 1)
	if st.State != ExportNone {
		t.Fatalf("status after delete = %+v", st)
	}
	matches, _ := filepath.Glob(filepath.Join(h.svc.exportsDir(h.ai.ID), "*"))
	if len(matches) != 0 {
		t.Fatalf("files left: %v", matches)
	}
}

func TestExportChecksToolsAndDiskFirst(t *testing.T) {
	h := newExportHarness(t)
	ctx := context.Background()
	h.svc.d.FreeDisk = func(string) (uint64, error) { return 10, nil }
	if _, err := h.svc.Export(ctx, h.ai.ID, 1); err == nil || !strings.Contains(err.Error(), "free disk space") {
		t.Fatalf("got %v", err)
	}
	h.svc.d.FreeDisk = nil
	h.svc.d.ExportTool = func(context.Context) (string, error) {
		return "", errors.New("this llama.cpp install has no llama-export-lora")
	}
	if _, err := h.svc.Export(ctx, h.ai.ID, 1); err == nil || !strings.Contains(err.Error(), "llama-export-lora") {
		t.Fatalf("got %v", err)
	}
	if _, err := h.svc.Export(ctx, h.ai.ID, 9); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown revision: %v", err)
	}
}
