package gguf

import (
	"slices"
	"testing"
)

func TestControlTokens(t *testing.T) {
	vocab := []struct {
		tok string
		typ uint32
	}{
		{"hello", 1}, {"<|im_start|>", 3}, {"<|im_end|>", 3}, {"<tool_call>", 4}, {"<s>", 3}, {"eos", 3}, {"▁world", 1},
	}
	path := writeGGUF(t, func(h *header) int {
		h.str("general.architecture")
		h.u32(typeString)
		h.str("qwen2")
		h.str("tokenizer.ggml.tokens")
		h.u32(typeArray)
		h.u32(typeString)
		h.u64(uint64(len(vocab)))
		for _, v := range vocab {
			h.str(v.tok)
		}
		h.str("tokenizer.ggml.token_type")
		h.u32(typeArray)
		h.u32(typeInt32)
		h.u64(uint64(len(vocab)))
		for _, v := range vocab {
			h.u32(v.typ)
		}
		return 3
	})
	got, err := ControlTokens(path)
	if err != nil {
		t.Fatal(err)
	}
	// User-defined tokens, such as Core's own <tool_call>, aren't control
	// tokens, and a control token spelled like a word isn't escaped.
	if want := []string{"<|im_start|>", "<|im_end|>", "<s>"}; !slices.Equal(got, want) {
		t.Fatalf("control tokens = %q, want %q", got, want)
	}
}
