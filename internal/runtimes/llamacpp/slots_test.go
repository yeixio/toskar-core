package llamacpp

import (
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// A chat model with slots starts with --parallel that many and a context of
// the window times the slots, so llama-server gives each slot the whole
// window (#1135). The cache type follows one slot's window, and embedding
// and reranking models take one slot.
func TestStartArgsSlots(t *testing.T) {
	for _, c := range []struct {
		cfg  pluginapi.ModelStartConfig
		want string
	}{
		{pluginapi.ModelStartConfig{ModelPath: "m.gguf", Context: 16384}, "--ctx-size 16384 --parallel 1"},
		{pluginapi.ModelStartConfig{ModelPath: "m.gguf", Context: 16384, Slots: 1}, "--ctx-size 16384 --parallel 1"},
		{pluginapi.ModelStartConfig{ModelPath: "m.gguf", Context: 16384, Slots: 2}, "--ctx-size 32768 --parallel 2"},
		{pluginapi.ModelStartConfig{ModelPath: "m.gguf", Slots: 3}, "--ctx-size 24576 --parallel 3"},
		{pluginapi.ModelStartConfig{ModelPath: "m.gguf", Context: 8192, Slots: 9}, "--ctx-size 32768 --parallel 4"},
	} {
		args := strings.Join(startArgs(c.cfg, 9000), " ")
		if !strings.HasSuffix(args, c.want) {
			t.Errorf("%+v: %q, want it to end %q", c.cfg, args, c.want)
		}
		if strings.Contains(args, "--cache-type-k") {
			t.Errorf("%+v: a window of 32K or less a slot keeps the f16 cache: %q", c.cfg, args)
		}
	}
	if args := strings.Join(startArgs(pluginapi.ModelStartConfig{ModelPath: "m.gguf", Context: 65536, Slots: 2}, 9000), " "); !strings.Contains(args, "--cache-type-k q8_0") || !strings.HasSuffix(args, "--ctx-size 131072 --parallel 2") {
		t.Errorf("64K a slot: %q", args)
	}
	embed := strings.Join(startArgs(pluginapi.ModelStartConfig{ModelPath: "e.gguf", Mode: pluginapi.ModeEmbedding, Slots: 3}, 9000), " ")
	if strings.Contains(embed, "--parallel 3") || chatSlots(pluginapi.ModelStartConfig{Mode: pluginapi.ModeEmbedding, Slots: 3}) != 1 {
		t.Errorf("embedding: %q", embed)
	}
}

// A running model with other slots starts again; a start that asks for none
// takes it as it runs.
func TestSameSlots(t *testing.T) {
	two := pluginapi.RunningModel{Slots: 2}
	old := pluginapi.RunningModel{}
	for _, c := range []struct {
		m    pluginapi.RunningModel
		cfg  pluginapi.ModelStartConfig
		same bool
	}{
		{two, pluginapi.ModelStartConfig{Slots: 2}, true},
		{two, pluginapi.ModelStartConfig{Slots: 1}, false},
		{two, pluginapi.ModelStartConfig{}, true},
		{old, pluginapi.ModelStartConfig{Slots: 1}, true},
		{old, pluginapi.ModelStartConfig{Slots: 2}, false},
		{two, pluginapi.ModelStartConfig{Slots: 1, Mode: pluginapi.ModeReranking}, true},
	} {
		if got := sameSlots(c.m, c.cfg); got != c.same {
			t.Errorf("running %d, asked %+v: same %v", c.m.Slots, c.cfg, got)
		}
	}
}
