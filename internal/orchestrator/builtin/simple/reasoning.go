package simple

import (
	"strings"

	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Keeping a reasoning model's thinking between tool calls (#841). The tool
// loop passes back only what the model wrote, so a reasoning model loses
// its plan after each call and works it out again. Within one turn, each
// tool step's thinking can go back with it; earlier turns never carry any,
// since the chat history keeps only answers.
//
// It's off until the quality set shows it helps, and TOSKAR_KEEP_REASONING
// turns it on for a run:
//   - "field" sends the thinking as the assistant message's
//     reasoning_content, for chat templates that render it, as DeepSeek's
//     recipe does for multi-step tool use.
//   - "inline" puts it before the step's call, as text, for templates that
//     drop reasoning_content from steps before the last user message, as
//     Qwen3's does, since tool results come back as user messages here.

// Ways of keeping the thinking.
const (
	reasoningOff    = ""
	reasoningField  = "field"
	reasoningInline = "inline"
)

// maxKeptReasoning is as much of one step's thinking as goes back, in runes:
// the end of it, where the plan for the next step usually is.
const maxKeptReasoning = 4000

// reasoningMode is TOSKAR_KEEP_REASONING, read each turn so a quality run
// can compare the modes. Tests replace it.
var reasoningMode = func() string {
	switch v := strings.ToLower(strings.TrimSpace(config.Env("KEEP_REASONING"))); v {
	case reasoningField, reasoningInline:
		return v
	case "1", "true", "on":
		return reasoningField
	}
	return reasoningOff
}

// keptReasoning is a tool step's assistant message with its thinking, as
// the mode keeps it.
func keptReasoning(m pluginapi.ChatMessage, thinking string) pluginapi.ChatMessage {
	thinking = strings.TrimSpace(thinking)
	if thinking == "" {
		return m
	}
	if r := []rune(thinking); len(r) > maxKeptReasoning {
		thinking = "…" + string(r[len(r)-maxKeptReasoning:])
	}
	switch reasoningMode() {
	case reasoningField:
		m.Reasoning = thinking
	case reasoningInline:
		m.Content = "My reasoning before this step:\n" + thinking + "\n\n" + m.Content
	}
	return m
}
