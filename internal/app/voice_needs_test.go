package app

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

// The Voice settings say which models voice mode and the doorbell still
// need, each with one to install, so an Admin isn't first told at the door
// (#667): none when a chat model and one that sees are installed; a model
// that sees when only its projector is missing; and a chat model on a
// computer with none.
func TestModelNeeds(t *testing.T) {
	ctx := context.Background()
	a, _, _ := pictureApp(t, true)
	if needs := a.modelNeeds(ctx); len(needs) != 0 {
		t.Fatalf("with both installed: %+v", needs)
	}

	a, _, _ = pictureApp(t, false)
	needs := a.modelNeeds(ctx)
	if len(needs) != 1 || needs[0].Ability != "vision" {
		t.Fatalf("without a model that sees: %+v", needs)
	}
	if want, size, ok := a.visionOption(ctx); ok && (needs[0].Model != want.ID || needs[0].SizeBytes != size || needs[0].Name == "") {
		t.Fatalf("offered %+v, want %s (%d bytes)", needs[0], want.ID, size)
	}

	// No stub model either: nothing to chat with.
	t.Setenv("TOSKAR_STUB_INFERENCE", "")
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	empty, err := New(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = empty.DB.Close() })
	needs = empty.modelNeeds(ctx)
	if len(needs) != 2 || needs[0].Ability != "chat" || needs[1].Ability != "vision" {
		t.Fatalf("on an empty computer: %+v", needs)
	}
	if needs[0].Model == "" || needs[0].SizeBytes == 0 {
		t.Fatalf("no chat model offered: %+v", needs[0])
	}
}
