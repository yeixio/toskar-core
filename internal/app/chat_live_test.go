package app

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// A live turn sends tokens for the answer and saves the same text (#1006).
func TestLiveChatSavesWhatItShowed(t *testing.T) {
	t.Setenv("TOSKAR_STUB_INFERENCE", "1")
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	a, err := New(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })
	a.StubReply = func(string, []pluginapi.ChatMessage) string { return "Juneau gets rain." }
	ctx := turnopts.WithLive(context.Background())
	conv, err := a.Conversations.Create(ctx, "live", "general-assistant", "auto")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := a.RunChat(ctx, "general-assistant", conv.ID, "Hi!", true, "auto", "")
	if err != nil {
		t.Fatal(err)
	}
	shown := ""
	for chunk := range stream {
		if chunk.Error != "" {
			t.Fatalf("error: %s", chunk.Error)
		}
		if chunk.Replace {
			shown = ""
		}
		shown += chunk.Content
	}
	if shown != "Juneau gets rain." {
		t.Fatalf("shown %q", shown)
	}
	msgs, err := a.Conversations.ListMessages(ctx, conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if last := msgs[len(msgs)-1]; last.Role != "assistant" || last.Content != shown {
		t.Fatalf("saved %+v", last)
	}
}

// Topic controls and portals see the answer only after its checks.
func TestMayShowEarly(t *testing.T) {
	ctx := context.Background()
	open := &chatExecEnv{}
	if !open.MayShowEarly(ctx) {
		t.Error("a plain profile keeps its answer back")
	}
	if open.MayShowEarly(turnopts.With(ctx, &turnopts.Options{Portal: "p1"})) {
		t.Error("a portal's answer shows early")
	}
	enforced := &chatExecEnv{profile: contracts.AIProfile{Topics: &contracts.TopicPolicy{StaysOn: "weather", Strictness: contracts.TopicsEnforce}}}
	if enforced.MayShowEarly(ctx) {
		t.Error("an enforcing profile's answer shows early")
	}
}
