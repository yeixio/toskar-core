package llamacpp

import (
	"context"
	"fmt"
	"strings"

	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/gguf"
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
// the model. A model with a draft guesses with both the draft and the chat;
// one with its own MTP head (mtp, #956), with that head and the chat.
func speculativeArgs(cfg pluginapi.ModelStartConfig, supported, mtp bool) []string {
	if !speculates(cfg, supported) {
		return nil
	}
	if cfg.Draft != "" {
		return []string{"-md", cfg.Draft, "-ngld", fmt.Sprintf("%d", defaultGPULayers()),
			"--spec-type", draftType + "," + speculativeType}
	}
	if mtp {
		return []string{"--spec-type", mtpType + "," + speculativeType}
	}
	return []string{"--spec-type", speculativeType}
}

// mtpType is llama-server's speculation with the model's own multi-token
// prediction head (#956): Qwen 3.8's -mtp files carry one, so the model
// guesses its next tokens itself and checks them, with no draft model.
const mtpType = "draft-mtp"

// usesMTP reports whether a model starts guessing with its own MTP head:
// TOSKAR_MTP is on, speculation is on, it has no draft, this llama-server
// has draft-mtp, and the file has the head.
//
// It's off unless asked for. Measured with Qwen 3.5 4B's MTP build on an
// M5 Pro (b12383, temperature 0), it was 38% faster on average, up to 70%,
// but changed 2 of 9 answers that off gave the same each time, and cut one
// short: an edit to two functions ended after the first. n-gram guessing,
// which checks several tokens at once too, changed none. Toskar's bar for
// guessing ahead is answers changed no more than two plain runs change
// them (#835).
func usesMTP(ctx context.Context, cfg pluginapi.ModelStartConfig, supported bool, server string) bool {
	switch strings.ToLower(strings.TrimSpace(config.Env("MTP"))) {
	case "on", "1", "true", "yes":
	default:
		return false
	}
	if !speculates(cfg, supported) || cfg.Draft != "" || !strings.Contains(serverHelp(ctx, server), mtpType) {
		return false
	}
	d, err := gguf.Inspect(cfg.ModelPath)
	return err == nil && d.MTP
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

// supportsSpeculation reports whether the llama-server at path lists
// speculativeType in its help: a build from before it would refuse to
// start with it.
func supportsSpeculation(ctx context.Context, path string) bool {
	help := serverHelp(ctx, path)
	return strings.Contains(help, "--spec-type") && strings.Contains(help, speculativeType)
}
