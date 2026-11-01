package app

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// A model that writes nothing doesn't end the turn as if it answered: it's
// asked once more, and when it writes nothing again the chat gets
// EMPTY_ANSWER, and no empty answer is saved (#553, #1001).
func TestEmptyAnswerFails(t *testing.T) {
	t.Setenv("TOSKAR_STUB_INFERENCE", "1")
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	a, err := New(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })
	var asked atomic.Int32
	a.StubReply = func(string, []pluginapi.ChatMessage) string { asked.Add(1); return "" }
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
	if asked.Load() != 2 {
		t.Errorf("asked %d times, want 2", asked.Load())
	}
	msgs, _ := a.Conversations.ListMessages(ctx, conv.ID)
	for _, m := range msgs {
		if m.Role == "assistant" {
			t.Fatalf("an empty answer was saved: %+v", m)
		}
	}
}

// When the second try answers, that answer is the turn's, with no error
// (#1001).
func TestEmptyAnswerAsksAgain(t *testing.T) {
	t.Setenv("TOSKAR_STUB_INFERENCE", "1")
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	a, err := New(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })
	var asked atomic.Int32
	a.StubReply = func(string, []pluginapi.ChatMessage) string {
		if asked.Add(1) == 1 {
			return ""
		}
		return "Heritage Coffee, on Front Street."
	}
	ctx := context.Background()
	conv, err := a.Conversations.Create(ctx, "empty", "general-assistant", "auto")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := a.RunChat(ctx, "general-assistant", conv.ID, "Find a cafe downtown", false, "auto", "")
	if err != nil {
		t.Fatal(err)
	}
	var text, failed string
	for chunk := range stream {
		text += chunk.Content
		failed += chunk.Error
	}
	if failed != "" || text != "Heritage Coffee, on Front Street." {
		t.Fatalf("text %q, error %q", text, failed)
	}
	msgs, _ := a.Conversations.ListMessages(ctx, conv.ID)
	var answers []string
	for _, m := range msgs {
		if m.Role == "assistant" {
			answers = append(answers, m.Content)
		}
	}
	if len(answers) != 1 || answers[0] != text {
		t.Errorf("saved %q", answers)
	}
}
