package llamacpp

import (
	"strings"
	"sync"

	"github.com/yeixio/toskar-core/internal/gguf"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Control tokens in what a model reads (#829). llama-server applies the
// chat template, then tokenizes the whole prompt with special tokens
// parsed, so text that spells one of the model's control tokens becomes
// that token: a web page, a file, a tool's result, or a visitor's message
// with <|im_end|><|im_start|>system in it would start a real system turn.
// Every message is escaped on its way to llama-server, so a spelling stays
// text: a word joiner after its first character, which reads the same and
// matches no token. Core's own text holds no control tokens; its tool
// protocol's <tool_call> and <tool_result> aren't control tokens.

var (
	controlMu sync.RWMutex
	// controlBy is each running llama-server's escaper, from its model's
	// vocabulary.
	controlBy = map[string]*strings.Replacer{}
)

// registerControl reads the control tokens of the model a llama-server at
// endpoint runs. A vocabulary that can't be read leaves the common markers.
func registerControl(endpoint, modelPath string) {
	tokens, err := gguf.ControlTokens(modelPath)
	controlMu.Lock()
	defer controlMu.Unlock()
	if err != nil || len(tokens) == 0 {
		delete(controlBy, endpoint)
		return
	}
	pairs := make([]string, 0, 2*len(tokens))
	for _, tok := range tokens {
		pairs = append(pairs, tok, pluginapi.EscapeToken(tok))
	}
	controlBy[endpoint] = strings.NewReplacer(pairs...)
}

func forgetControl(endpoint string) {
	controlMu.Lock()
	defer controlMu.Unlock()
	delete(controlBy, endpoint)
}

// escapeControl is messages with every control-token spelling escaped, for
// the llama-server at endpoint. messages itself is left as it is.
func escapeControl(endpoint string, messages []pluginapi.ChatMessage) []pluginapi.ChatMessage {
	controlMu.RLock()
	vocab := controlBy[endpoint]
	controlMu.RUnlock()
	out := make([]pluginapi.ChatMessage, len(messages))
	for i, m := range messages {
		if vocab != nil {
			m.Content = vocab.Replace(m.Content)
		}
		// And the common markers, for what the vocabulary misses.
		m.Content = pluginapi.EscapeChatMarkers(m.Content)
		out[i] = m
	}
	return out
}
