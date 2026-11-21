package app

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

// connectCamera marks a Reolink camera connected, as connecting one does.
func connectCamera(t *testing.T, a *App) {
	t.Helper()
	if _, err := a.DB.SQL.Exec(`INSERT INTO connections (service_id, connected_at, checked_at, status) VALUES ('reolink', '2026-11-19T00:00:00Z', '2026-11-19T00:00:00Z', 'ok')`); err != nil {
		t.Fatal(err)
	}
}

// The Voice settings say which models voice mode and the doorbell still
// need, each with one to install, so an Admin isn't first told at the door
// (#667): none when a chat model and one that sees are installed; a model
// that sees when only its projector is missing, once a camera is connected
// (#1343); and on a computer with none, one model that chats and sees.
func TestModelNeeds(t *testing.T) {
	ctx := context.Background()
	a, _, _ := pictureApp(t, true)
	connectCamera(t, a)
	if needs := a.modelNeeds(ctx); len(needs) != 0 {
		t.Fatalf("with both installed: %+v", needs)
	}

	a, _, _ = pictureApp(t, false)
	if needs := a.modelNeeds(ctx); len(needs) != 0 {
		t.Fatalf("without a camera: %+v", needs)
	}
	connectCamera(t, a)
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
	if len(needs) != 1 || needs[0].Ability != "chat" || needs[0].Model == "" || needs[0].SizeBytes == 0 {
		t.Fatalf("on an empty computer with no camera: %+v", needs)
	}
	connectCamera(t, empty)
	needs = empty.modelNeeds(ctx)
	if len(needs) != 1 || needs[0].Ability != "both" || needs[0].Model == "" {
		t.Fatalf("on an empty computer with a camera: %+v", needs)
	}
	m, err := empty.Models.Get(ctx, needs[0].Model)
	if err != nil || !m.Capabilities.Vision || m.DownloadBytes <= m.SizeBytes || needs[0].SizeBytes != int64(m.DownloadBytes) {
		t.Fatalf("offered %+v: %+v, %v", needs[0], m, err)
	}
}
