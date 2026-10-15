package app

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// A chat's task ends completed when the reply is done, so it stops counting
// as active, and raises no "task finished" notice of its own.
func TestChatTaskEndsCompleted(t *testing.T) {
	t.Setenv("TOSKAR_STUB_INFERENCE", "1")
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	a, err := New(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })
	a.StubReply = func(string, []pluginapi.ChatMessage) string { return "Hello there." }
	ctx := context.Background()
	conv, err := a.Conversations.Create(ctx, "task check", "general-assistant", "auto")
	if err != nil {
		t.Fatal(err)
	}
	subID, evs := a.Bus.Subscribe()
	defer a.Bus.Unsubscribe(subID)
	stream, err := a.RunChat(ctx, "general-assistant", conv.ID, "Hi! How are you?", false, "auto", "")
	if err != nil {
		t.Fatal(err)
	}
	for range stream {
	}
	list, err := a.Tasks.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Status != contracts.TaskCompleted {
		t.Fatalf("tasks: %+v", list)
	}
	for {
		select {
		case evt := <-evs:
			if evt.Type == events.TaskCompleted || evt.Type == events.TaskFailed {
				t.Fatalf("a chat raised %s", evt.Type)
			}
			continue
		default:
		}
		break
	}
}

// Tasks an earlier run left unfinished, such as chats from before tasks
// were settled, are ended at the next start.
func TestSettleInterrupted(t *testing.T) {
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	a, err := New(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })
	ctx := context.Background()
	stuck, _ := a.Tasks.Create(ctx, "", "", "an old chat")
	done, _ := a.Tasks.Create(ctx, "", "", "a finished chat")
	a.Tasks.SetChatStatus(ctx, done.ID, contracts.TaskCompleted, contracts.Failure{})
	if n, err := a.Tasks.SettleInterrupted(ctx); err != nil || n != 1 {
		t.Fatalf("settled %d, %v", n, err)
	}
	got, _ := a.Tasks.Get(ctx, stuck.ID)
	if got.Status != contracts.TaskFailed || got.ErrorCode != "TASK_INTERRUPTED" {
		t.Errorf("stuck task: %+v", got)
	}
	// A failed chat keeps its error's code and values (#588).
	failed, _ := a.Tasks.Create(ctx, "", "", "a chat that failed")
	a.Tasks.SetChatStatus(ctx, failed.ID, contracts.TaskFailed,
		contracts.Failure{Text: "out of memory", Code: "OUT_OF_MEMORY", Details: map[string]any{"model_id": "qwen3"}})
	if got, _ := a.Tasks.Get(ctx, failed.ID); got.ErrorCode != "OUT_OF_MEMORY" || got.ErrorDetails["model_id"] != "qwen3" {
		t.Errorf("failed task: %+v", got)
	}
	if got, _ := a.Tasks.Get(ctx, done.ID); got.Status != contracts.TaskCompleted {
		t.Errorf("finished task changed: %+v", got)
	}
}

// A chat portal's turn keeps its own profile and language, whatever the
// conversation was set to (#205).
func TestFixedProfileTurn(t *testing.T) {
	t.Setenv("TOSKAR_STUB_INFERENCE", "1")
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	a, err := New(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })
	var system string
	a.StubReply = func(_ string, msgs []pluginapi.ChatMessage) string {
		for _, m := range msgs {
			if m.Role == "system" {
				system += m.Content
			}
		}
		return "Hola."
	}
	ctx := context.Background()
	conv, err := a.Conversations.Create(ctx, "portal", "programming", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := turnopts.With(ctx, &turnopts.Options{Knowledge: true, FixedProfile: true, Language: "es"})
	stream, err := a.RunChat(turn, "", conv.ID, "Hello", false, "", "automatic")
	if err != nil {
		t.Fatal(err)
	}
	for range stream {
	}
	list, _ := a.Tasks.List(ctx)
	if len(list) != 1 || list[0].ProfileID != "general-assistant" {
		t.Fatalf("tasks: %+v", list)
	}
	if !strings.Contains(system, "Write your reply in Spanish") {
		t.Fatalf("no Spanish instruction in %q", system)
	}
}
