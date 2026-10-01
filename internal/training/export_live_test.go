package training

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TestExportWithRealLlamaCpp merges a real adapter with llama-export-lora.
// It is skipped unless YGG_TEST_EXPORT_TOOL is llama-export-lora,
// YGG_TEST_BASE_GGUF a base model, and YGG_TEST_ADAPTER_GGUF a LoRA adapter
// for it.
func TestExportWithRealLlamaCpp(t *testing.T) {
	tool, base, adapter := os.Getenv("YGG_TEST_EXPORT_TOOL"), os.Getenv("YGG_TEST_BASE_GGUF"), os.Getenv("YGG_TEST_ADAPTER_GGUF")
	if tool == "" || base == "" || adapter == "" {
		t.Skip("set YGG_TEST_EXPORT_TOOL, YGG_TEST_BASE_GGUF, and YGG_TEST_ADAPTER_GGUF to run against llama.cpp")
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
	raw, err := os.ReadFile(adapter)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rev.adapterPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	h.svc.d.ExportTool = func(context.Context) (string, error) { return tool, nil }
	h.svc.d.ModelPath = func(context.Context, string) (string, error) { return base, nil }

	st, err := h.svc.Export(ctx, ai.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	estimate := st.SizeBytes
	h.svc.Wait()
	st, _ = h.svc.ExportStatus(ctx, ai.ID, 1)
	if st.State != ExportReady {
		t.Fatalf("status = %+v", st)
	}
	t.Logf("estimated %d bytes, wrote %d", estimate, st.SizeBytes)
	if st.SizeBytes > estimate {
		t.Errorf("the merged file is larger than the disk estimate")
	}
	path, _, _ := h.svc.ExportFile(ctx, ai.ID, 1)
	merged, err := readGGUFTensors(path)
	if err != nil {
		t.Fatal(err)
	}
	original, err := readGGUFTensors(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged) != len(original) {
		t.Fatalf("merged has %d tensors, base %d", len(merged), len(original))
	}
	for _, m := range merged {
		if strings.Contains(m.Name, "lora") {
			t.Fatalf("adapter tensor %s left in the merged model", m.Name)
		}
	}
}
