package gguf_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/yeixio/toskar-core/internal/gguf"
	"github.com/yeixio/toskar-core/internal/gguf/gguftest"
)

// Inspect reads what an added model says about itself, and refuses a file
// that isn't whole (#467).
func TestInspect(t *testing.T) {
	dir := t.TempDir()
	path := gguftest.Write(t, dir, "My-Model-Q4_K_M.gguf", gguftest.Options{Name: "My Model", FileType: 15, Context: 8192, Elements: 256})
	d, err := gguf.Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "My Model" || d.Architecture != "llama" || d.Quantization != "Q4_K_M" || d.ContextLength != 8192 || d.Parameters != 256 || d.Projector() {
		t.Errorf("details = %+v", d)
	}

	// No file type in the header: the name says Q5_K_S.
	if d, err := gguf.Inspect(gguftest.Write(t, dir, "other.Q5_K_S.gguf", gguftest.Options{})); err != nil || d.Quantization != "Q5_K_S" {
		t.Errorf("from the name: %+v, %v", d, err)
	}
	if d, _ := gguf.Inspect(gguftest.Write(t, dir, "mmproj.gguf", gguftest.Options{Architecture: "clip", Type: "mmproj"})); !d.Projector() {
		t.Error("a projector isn't seen as one")
	}

	for name, cut := range map[string]int{"data": 8, "header": 300} {
		if _, err := gguf.Inspect(gguftest.Write(t, dir, name+".gguf", gguftest.Options{Name: "Cut", Cut: cut})); !errors.Is(err, gguf.ErrTruncated) {
			t.Errorf("cut in the %s: err = %v", name, err)
		}
	}
	notGGUF := filepath.Join(dir, "notes.gguf")
	if err := os.WriteFile(notGGUF, []byte("hello, this is text"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gguf.Inspect(notGGUF); err == nil || err.Error() != "not a GGUF file" {
		t.Errorf("text file: %v", err)
	}
}

// A model that thinks before it answers is known by its chat template, or,
// for DeepSeek R1's distills, whose templates don't say, by its name (#840).
// tests/thinking checks the real files of each family.
func TestInspectThinks(t *testing.T) {
	dir := t.TempDir()
	for i, tc := range []struct {
		name, template string
		want           bool
	}{
		{"Qwen3 8B", "{%- if enable_thinking is defined and enable_thinking is false %}{{- '<think>\\n\\n</think>\\n\\n' }}{%- endif %}", true},
		{"gpt-oss-20b", "{{- '<|start|>assistant<|channel|>analysis<|message|>' }}", true},
		{"DeepSeek R1 Distill Qwen 1.5B", "{{'<｜Assistant｜>'}}", true},
		{"QwQ 32B", "", true},
		{"Qwen2.5 32B Instruct", "{{- '<|im_start|>assistant\\n' }}", false},
		{"Llama 3.2 1B Instruct", "{{- '<|start_header_id|>assistant<|end_header_id|>' }}", false},
		{"Reasoning Helper 7B", "", false},
	} {
		path := gguftest.Write(t, dir, fmt.Sprintf("m%d.gguf", i), gguftest.Options{Name: tc.name, ChatTemplate: tc.template})
		d, err := gguf.Inspect(path)
		if err != nil {
			t.Fatal(err)
		}
		if d.Thinks != tc.want {
			t.Errorf("%s: thinks = %v, want %v", tc.name, d.Thinks, tc.want)
		}
		if d.ChatTemplate != (tc.template != "") {
			t.Errorf("%s: chat template = %v", tc.name, d.ChatTemplate)
		}
	}
}
