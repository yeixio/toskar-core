package simple

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// offlineEnv fails every look-up as a computer with no internet does.
type offlineEnv struct {
	scriptedEnv
	calls []string
}

func (e *offlineEnv) ExecuteTool(ctx context.Context, toolID string, args map[string]any) (map[string]any, error) {
	e.calls = append(e.calls, toolID)
	lookup := &net.DNSError{Err: "no such host", Name: "toskar.ai", IsNotFound: true}
	if toolID == "news.read" {
		return nil, errors.Join(errors.New("the AI news couldn't be read from toskar.ai"), lookup)
	}
	return nil, fmt.Errorf("Get \"https://html.duckduckgo.com/html/\": dial tcp: %w", lookup)
}

func offlineRun(t *testing.T, env *offlineEnv, prompt string, tools ...contracts.ToolPolicy) string {
	t.Helper()
	events, err := New().Run(context.Background(), contracts.Task{Prompt: prompt}, contracts.AIProfile{
		Roles: []contracts.ModelRole{{Role: "assistant", ModelID: "m"}},
		Tools: tools,
	}, env)
	if err != nil {
		t.Fatal(err)
	}
	var text string
	for evt := range events {
		if evt.Type == "agent.message" {
			text += evt.Content
		}
	}
	return text
}

// Offline, a model told the news couldn't be read wrote the day's news
// anyway (#965). The answer is asked for as saying it's offline instead.
func TestOfflineNewsIsSaidNotMadeUp(t *testing.T) {
	env := &offlineEnv{scriptedEnv: scriptedEnv{replies: []string{
		"You're offline, so I couldn't get today's AI news.",
	}}}
	text := offlineRun(t, env, "Tell me about the AI news", contracts.ToolPolicy{ToolID: "news.read", Policy: "allow"})
	if len(env.calls) != 1 || env.calls[0] != "news.read" || !strings.Contains(text, "offline") {
		t.Fatalf("calls=%v text=%q", env.calls, text)
	}
	last := env.seen[len(env.seen)-1]
	ask := last[len(last)-1].Content
	if !strings.Contains(ask, "isn't connected to the internet") || !strings.Contains(ask, "the AI news") || !strings.Contains(ask, "Don't answer the question from memory") {
		t.Fatalf("not asked to say it's offline: %q", ask)
	}
}

// A message that's looked up first, offline, gets the same answer, and a
// model that writes nothing still gets one.
func TestOfflineLookupFirstSaysSo(t *testing.T) {
	env := &offlineEnv{scriptedEnv: scriptedEnv{replies: []string{""}}}
	text := offlineRun(t, env, "Search the web for the latest llama.cpp release", contracts.ToolPolicy{ToolID: "internet.search", Policy: "allow"})
	if len(env.calls) == 0 || env.calls[0] != "internet.search" {
		t.Fatalf("not looked up first: %v", env.calls)
	}
	if text != offlineFallback {
		t.Fatalf("text=%q", text)
	}
}

// A page that can't be opened isn't the computer being offline: a
// mistyped address fails the same way.
func TestOpeningAPageDoesntCountAsOffline(t *testing.T) {
	ctx := withLookups(context.Background())
	env := &offlineEnv{}
	_, _ = runTool(ctx, env, "internet.open", map[string]any{"url": "https://exampel.invalid"})
	if lookupsFrom(ctx).cutOff() {
		t.Fatal("internet.open counted as offline")
	}
	_, _ = runTool(ctx, env, "internet.search", map[string]any{"query": "x"})
	if !lookupsFrom(ctx).cutOff() {
		t.Fatal("a failed search wasn't counted")
	}
}

// A tool the model calls in the loop, offline, ends the turn the same way.
func TestOfflineToolCallInTheLoop(t *testing.T) {
	env := &offlineEnv{scriptedEnv: scriptedEnv{replies: []string{
		`{"tool_call":{"id":"internet.search","args":{"query":"Juneau weather"}}}`,
		"This computer is offline, so I couldn't look up the weather.",
	}}}
	text := offlineRun(t, env, "weather", contracts.ToolPolicy{ToolID: "internet.search", Policy: "ask"})
	if len(env.calls) != 1 || env.calls[0] != "internet.search" || !strings.Contains(text, "offline") {
		t.Fatalf("calls=%v text=%q", env.calls, text)
	}
	if len(env.seen) != 2 {
		t.Fatalf("model asked %d times, want the call and the offline answer", len(env.seen))
	}
}
