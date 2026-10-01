package training

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

type pageReader struct{}

func (pageReader) RecognizePDF(_ context.Context, _ []byte, pages []int) (map[int]string, error) {
	out := map[int]string{}
	for _, p := range pages {
		out[p] = "Store Hours\nSunday: closed"
	}
	return out, nil
}

// A scanned PDF has no examples, so it becomes knowledge, read with text
// recognition, and cannot be used for training.
func TestScannedPDFMaterialBecomesKnowledge(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "mimir", "testdata", "scanned.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	content := base64.StdEncoding.EncodeToString(raw)
	name, text, err := MaterialText("hours.pdf", "", content)
	if err != nil || name != "hours.txt" || text != "" {
		t.Fatalf("MaterialText = %q %q %v", name, text, err)
	}

	h := newHarness(t, "ok")
	h.kb.SetRecognizer(pageReader{})
	ctx := context.Background()
	ai, err := h.svc.CreateAI(ctx, CreateInput{Name: "Tire Bot", BaseModelID: "small-q4"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.AddMaterial(ctx, ai.ID, MaterialInput{Filename: "hours.pdf", ContentBase64: content, Use: UseTraining}); err == nil {
		t.Fatal("a scanned PDF was accepted as training examples")
	}
	m, err := h.svc.AddMaterial(ctx, ai.ID, MaterialInput{Filename: "hours.pdf", ContentBase64: content})
	if err != nil {
		t.Fatal(err)
	}
	if m.Use != UseKnowledge || m.KnowledgeSourceID == "" {
		t.Fatalf("material = %+v", m)
	}
	src, err := h.kb.Get(ctx, m.KnowledgeSourceID)
	if err != nil || src.ChunkCount != 1 {
		t.Fatalf("source = %+v, %v", src, err)
	}
}
