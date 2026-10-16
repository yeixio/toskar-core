package app

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// A model that writes nothing doesn't end the turn as if it answered: the
// chat gets EMPTY_ANSWER, and no empty answer is saved (#553).
func TestEmptyAnswerFails(t *testing.T) {
	t.Setenv("TOSKAR_STUB_INFERENCE", "1")
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	a, err := New(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })
	a.StubReply = func(string, []pluginapi.ChatMessage) string { return "" }
	ctx := context.Background()
	conv, err := a.Conversations.Create(ctx, "empty", "general-assistant", "auto")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := a.RunChat(ctx, "general-assistant", conv.ID, "Hi!", false, "auto", "")
	if err != nil {
		t.Fatal(err)
	}
	var code string
	for chunk := range stream {
		if chunk.Error != "" {
			code = chunk.Code
		}
	}
	if code != "EMPTY_ANSWER" {
		t.Fatalf("error code %q", code)
	}
	msgs, _ := a.Conversations.ListMessages(ctx, conv.ID)
	for _, m := range msgs {
		if m.Role == "assistant" {
			t.Fatalf("an empty answer was saved: %+v", m)
		}
	}
}
