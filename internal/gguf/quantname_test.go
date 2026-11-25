package gguf

import "testing"

// An upload's random name never reads as a quantization: a dashed UUID
// can have a "bf16" segment, so uploads drop the dashes (models.UploadPath).
func TestQuantFromUploadName(t *testing.T) {
	if q := quantFromName("upload-3f2a1c0e-bf16-4d2e-9a1b-0c3d4e5f6a7b.gguf"); q != "BF16" {
		t.Fatalf("a dashed UUID reads as %q", q)
	}
	if q := quantFromName("upload-3f2a1c0ebf164d2e9a1b0c3d4e5f6a7b.gguf"); q != "" {
		t.Fatalf("an upload name without dashes reads as %q", q)
	}
}
