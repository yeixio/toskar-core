package dlmirror

import (
	"reflect"
	"testing"
)

func TestURLs(t *testing.T) {
	const hf = "https://huggingface.co/ggml-org/gemma-3-4b-it-GGUF/resolve/main/gemma-3-4b-it-Q4_K_M.gguf"
	cases := []struct {
		name, mirror, raw string
		want              []string
	}{
		{"no mirror", "", hf, []string{hf}},
		{"mirror first", "http://cache.lan:8380", hf, []string{"http://cache.lan:8380/hf/ggml-org/gemma-3-4b-it-GGUF/resolve/main/gemma-3-4b-it-Q4_K_M.gguf", hf}},
		{"trailing slash", "http://cache.lan:8380/ ", hf, []string{"http://cache.lan:8380/hf/ggml-org/gemma-3-4b-it-GGUF/resolve/main/gemma-3-4b-it-Q4_K_M.gguf", hf}},
		{"not a URL", "cache.lan:8380", hf, []string{hf}},
		{"other source", "http://cache.lan:8380", "https://example.com/model.gguf", []string{"https://example.com/model.gguf"}},
		{"look-alike host", "http://cache.lan:8380", "https://huggingface.co.example.com/x.gguf", []string{"https://huggingface.co.example.com/x.gguf"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(Env, c.mirror)
			if got := URLs(c.raw); !reflect.DeepEqual(got, c.want) {
				t.Errorf("URLs(%q) = %q, want %q", c.raw, got, c.want)
			}
		})
	}
}
