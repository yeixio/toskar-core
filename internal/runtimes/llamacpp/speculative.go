package llamacpp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// speculativeType is llama-server's n-gram speculation (#835): it guesses
// the next tokens from text already in the chat, and the model checks a few
// at once. It needs no draft model and no memory. Measured on Qwen 2.5 7B
// on Apple silicon at temperature 0, it answered every prompt the same as
// without it, was 0 to 7% faster on new text, and 42% faster editing code
// and 99% faster editing a letter the person pasted in. ngram-cache, the
// other kind that needs no model, was up to 16% slower on new text.
const speculativeType = "ngram-mod"

// speculativeArgs are the flags that turn speculation on for a chat model,
// or none: for embedding and reranking, for a model that sees pictures
// (not measured with a projector yet), when this llama-server doesn't have
// it, when TOSKAR_SPECULATIVE is off, or when the person turned it off for
// the model. A model with a draft guesses with both the draft and the chat.
func speculativeArgs(cfg pluginapi.ModelStartConfig, supported bool) []string {
	if !speculates(cfg, supported) {
		return nil
	}
	if cfg.Draft != "" {
		return []string{"-md", cfg.Draft, "-ngld", fmt.Sprintf("%d", defaultGPULayers()),
			"--spec-type", draftType + "," + speculativeType}
	}
	return []string{"--spec-type", speculativeType}
}

// draftType is llama-server's speculation with a draft model: a small model
// of the same family writes ahead and the big one checks its guesses. On an
// RX 7900 XTX with Qwen 2.5 and its 0.5B draft, with speculativeType as
// well, it made the 32B 87% faster on average, the 14B 56%, and the 7B 20%,
// and changed answers no more than the GPU changes them between two plain
// runs. Every build that has speculativeType has it.
const draftType = "draft-simple"

// draftFor is the draft a model starts with: cfg.Draft when it speculates.
func draftFor(cfg pluginapi.ModelStartConfig, supported bool) string {
	if !speculates(cfg, supported) {
		return ""
	}
	return cfg.Draft
}

func speculates(cfg pluginapi.ModelStartConfig, supported bool) bool {
	if cfg.Mode != "" || cfg.Projector != "" || cfg.NoSpeculation || !supported {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(config.Env("SPECULATIVE"))) {
	case "off", "0", "false", "no":
		return false
	}
	return true
}

// specSupport remembers, per llama-server binary, whether it has
// speculativeType: a build from before it would refuse to start with it.
var specSupport struct {
	sync.Mutex
	known map[string]specProbe
}

type specProbe struct {
	modTime   time.Time
	supported bool
}

// specHelp prints a llama-server's help; a variable so tests can stand in.
var specHelp = func(ctx context.Context, path string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, path, "--help").CombinedOutput()
}

// supportsSpeculation reports whether the llama-server at path lists
// speculativeType in its help. It asks once per binary, and again when the
// binary is replaced by an update.
func supportsSpeculation(ctx context.Context, path string) bool {
	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	specSupport.Lock()
	defer specSupport.Unlock()
	if p, ok := specSupport.known[path]; ok && p.modTime.Equal(st.ModTime()) {
		return p.supported
	}
	out, _ := specHelp(ctx, path)
	supported := strings.Contains(string(out), "--spec-type") && strings.Contains(string(out), speculativeType)
	if specSupport.known == nil {
		specSupport.known = map[string]specProbe{}
	}
	specSupport.known[path] = specProbe{modTime: st.ModTime(), supported: supported}
	return supported
}
