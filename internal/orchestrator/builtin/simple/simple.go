package simple

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yeixio/yggdrasil-core/internal/contextusage"
	"github.com/yeixio/yggdrasil-core/internal/events"
	"github.com/yeixio/yggdrasil-core/internal/mimir"
	"github.com/yeixio/yggdrasil-core/internal/tools"
	"github.com/yeixio/yggdrasil-core/pkg/contracts"
	"github.com/yeixio/yggdrasil-core/pkg/pluginapi"
)

const id = "simple"
const maxToolCalls = 10

// Orchestrator runs single-model chat with optional tools.
type Orchestrator struct{}

func New() *Orchestrator { return &Orchestrator{} }

func (o *Orchestrator) ID() string          { return id }
func (o *Orchestrator) DisplayName() string { return "Simple" }

func (o *Orchestrator) Capabilities() pluginapi.OrchestratorCapabilities {
	return pluginapi.OrchestratorCapabilities{
		SupportsTools: true,
		SupportsTeam:  false,
		Roles:         []string{"assistant", "worker"},
	}
}

func (o *Orchestrator) ValidateProfile(ctx context.Context, profile contracts.AIProfile) error {
	if len(profile.Roles) == 0 {
		return fmt.Errorf("simple orchestrator requires at least one role")
	}
	hasModel := false
	for _, r := range profile.Roles {
		if r.ModelID != "" {
			hasModel = true
			break
		}
	}
	if !hasModel {
		return fmt.Errorf("simple orchestrator requires a model assignment")
	}
	return nil
}

func (o *Orchestrator) Run(
	ctx context.Context,
	task contracts.Task,
	profile contracts.AIProfile,
	env pluginapi.ExecutionEnvironment,
) (<-chan pluginapi.OrchestrationEvent, error) {
	role := pickRole(profile.Roles)
	ch := make(chan pluginapi.OrchestrationEvent, 8)
	go func() {
		defer close(ch)
		ch <- pluginapi.OrchestrationEvent{Type: "agent.started", Role: role}

		instructions := "Format answers in Markdown with short paragraphs, lists, and links. Do not wrap the whole answer in a code fence."
		reference := referenceMaterial(ctx, env, task.Prompt)
		if found, ok := lookUpFirst(ctx, env, profile, task.Prompt); ok {
			reference = joinReference(reference, found)
			instructions += "\n" + lookupGuidance
			profile = withoutWeb(profile)
		}
		toolPrompt := tools.PromptFor(profile)
		sys := instructions
		if toolPrompt != "" {
			sys += "\n" + toolPrompt
		}
		if extra := turnGuidance(ctx, env, task.Prompt); extra != "" {
			sys = extra + "\n\n" + sys
		}
		messages := []pluginapi.ChatMessage{{Role: "system", Content: sys}}
		if prior := priorMessages(ctx, env, task.Prompt, sys); len(prior) > 0 {
			messages = append(messages, prior...)
		}
		messages = append(messages, pluginapi.ChatMessage{Role: "user", Content: withReference(task.Prompt, reference)})

		var metrics *pluginapi.GenerationMetrics
		var usage contextusage.Usage
		toolsOn := len(tools.Enabled(profile, nil)) > 0
		nodeID, _ := env.NodeForRole(role)
		calls := 0
		malformed := 0

		for {
			content, m, err := generateText(ctx, env, role, messages)
			if err != nil {
				if content != "" {
					ch <- pluginapi.OrchestrationEvent{Type: "agent.message", Role: role, NodeID: nodeID, Content: content}
				}
				ch <- pluginapi.OrchestrationEvent{Type: "agent.error", Role: role, Error: err.Error(), Done: true}
				return
			}
			if m != nil {
				metrics = m
			}
			promptTokens := 0
			if m != nil {
				promptTokens = m.PromptTokens
			}
			usage = contextusage.Measure(instructions, toolPrompt, messages, promptTokens)

			parsed := tools.ParseModelOutput(content)
			if parsed.Sanitized {
				env.Emit(events.ToolParsed, map[string]any{"sanitized": true})
			}
			if parsed.Call != nil && toolsOn {
				if calls >= maxToolCalls {
					streamText(ch, role, nodeID, "I stopped after "+fmt.Sprint(maxToolCalls)+" tool calls so this request would not loop.", metrics, usage)
					return
				}
				calls++
				env.Emit(events.ToolParsed, map[string]any{
					"accepted": true,
					"tool_id":  parsed.Call.ID,
					"format":   parsed.Format,
				})
				if parsed.Text != "" {
					ch <- pluginapi.OrchestrationEvent{Type: "agent.message", Role: role, NodeID: nodeID, Content: parsed.Text}
				}
				result, err := env.ExecuteTool(ctx, parsed.Call.ID, parsed.Call.Args)
				var resultNote string
				if err != nil {
					payload, _ := json.Marshal(map[string]any{"ok": false, "error": publicToolError(err)})
					resultNote = "The tool failed. Details for you, not for the user:\n" + string(payload)
				} else {
					raw, _ := json.Marshal(map[string]any{"ok": true, "result": result})
					resultNote = "Tool result for you, not for the user. " + untrustedNote + "\n" + string(raw)
				}
				followUp := answerAfterTools
				if err == nil && parsed.Call.ID == "internet.search" && calls < maxToolCalls {
					if page, opened := followLiveSearch(ctx, env, profile, task.Prompt, parsed.Call.Args, result); opened {
						calls++
						raw, _ := json.Marshal(map[string]any{"ok": true, "result": page})
						resultNote += "\n\nPage for you, not for the user. " + untrustedNote + "\n" + string(raw)
						followUp = answerFromPage
					}
				}
				messages = append(messages,
					pluginapi.ChatMessage{Role: "assistant", Content: content},
					pluginapi.ChatMessage{Role: "user", Content: resultNote + followUp},
				)
				continue
			}
			if toolsOn && parsed.Malformed && malformed < 2 && calls < maxToolCalls {
				malformed++
				env.Emit(events.ToolParsed, map[string]any{"accepted": false, "format": "rejected", "error": "invalid tool call"})
				env.Emit(events.ToolFailed, map[string]any{"malformed": true, "error": "invalid tool call"})
				messages = append(messages,
					pluginapi.ChatMessage{Role: "assistant", Content: content},
					pluginapi.ChatMessage{Role: "user", Content: "That tool call was not valid and was not run. Reply with one JSON object {\"tool_call\":{\"id\":\"...\",\"args\":{}}} using a listed tool, or answer in plain text."},
				)
				continue
			}
			streamText(ch, role, nodeID, parsed.Text, metrics, usage)
			return
		}
	}()
	return ch, nil
}

func generateText(
	ctx context.Context,
	env pluginapi.ExecutionEnvironment,
	role string,
	messages []pluginapi.ChatMessage,
) (string, *pluginapi.GenerationMetrics, error) {
	stream, err := env.Generate(ctx, role, messages)
	if err != nil {
		return "", nil, err
	}
	var b strings.Builder
	var metrics *pluginapi.GenerationMetrics
	for chunk := range stream {
		if chunk.Error != "" {
			return b.String(), metrics, fmt.Errorf("%s", chunk.Error)
		}
		if chunk.Metrics != nil {
			metrics = chunk.Metrics
		}
		if chunk.Content != "" {
			b.WriteString(chunk.Content)
		}
		if chunk.Done {
			break
		}
	}
	return b.String(), metrics, nil
}

func streamText(ch chan<- pluginapi.OrchestrationEvent, role, nodeID, content string, metrics *pluginapi.GenerationMetrics, usage contextusage.Usage) {
	visible := tools.VisibleText(content)
	if visible != "" {
		ch <- pluginapi.OrchestrationEvent{Type: "agent.message", Role: role, NodeID: nodeID, Content: visible}
	}
	ch <- pluginapi.OrchestrationEvent{
		Type:    "agent.completed",
		Role:    role,
		NodeID:  nodeID,
		Content: visible,
		Metrics: metrics,
		Payload: map[string]any{"context": usage.Map()},
		Done:    true,
	}
}

// untrustedNote tells the model that retrieved content is data (§58).
const untrustedNote = mimir.UntrustedNote

// referenceSource is implemented by environments that retrieve reference
// material, such as connected knowledge, for a turn.
type referenceSource interface {
	ReferenceMaterial(ctx context.Context, prompt string) string
}

func referenceMaterial(ctx context.Context, env pluginapi.ExecutionEnvironment, prompt string) string {
	src, ok := env.(referenceSource)
	if !ok {
		return ""
	}
	return strings.TrimSpace(src.ReferenceMaterial(ctx, prompt))
}

func withReference(prompt, reference string) string { return mimir.WithReference(prompt, reference) }

// turnInstructions is implemented by environments that add instructions for
// one turn: a specialized AI's system instructions and connected knowledge.
type turnInstructions interface {
	TurnInstructions(ctx context.Context, prompt string) string
}

func turnGuidance(ctx context.Context, env pluginapi.ExecutionEnvironment, prompt string) string {
	src, ok := env.(turnInstructions)
	if !ok {
		return ""
	}
	return strings.TrimSpace(src.TurnInstructions(ctx, prompt))
}

type conversationMemory interface {
	PriorMessages(ctx context.Context) []pluginapi.ChatMessage
	ContextLimit() int
}

func priorMessages(ctx context.Context, env pluginapi.ExecutionEnvironment, prompt, reserved string) []pluginapi.ChatMessage {
	src, ok := env.(conversationMemory)
	if !ok {
		return nil
	}
	limit := src.ContextLimit()
	if limit <= 0 {
		limit = contextusage.DefaultWindow
	}
	return contextusage.FitPrior(src.PriorMessages(ctx), contextusage.RoomRunes(limit, reserved, prompt))
}

func publicToolError(err error) string {
	msg := strings.TrimSpace(err.Error())
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	if len(msg) > 240 {
		msg = msg[:240]
	}
	return msg
}

func parseToolCall(content string) (toolID string, args map[string]any, ok bool) {
	parsed := tools.ParseModelOutput(content)
	if parsed.Call == nil {
		return "", nil, false
	}
	return parsed.Call.ID, parsed.Call.Args, true
}

func userVisibleReply(content string) string {
	return tools.VisibleText(content)
}

func pickRole(roles []contracts.ModelRole) string {
	for _, r := range roles {
		if r.Required && r.Role != "" {
			return r.Role
		}
	}
	if len(roles) > 0 {
		return roles[0].Role
	}
	return "assistant"
}
