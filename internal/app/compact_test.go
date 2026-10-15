package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/internal/muninn"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/store/repositories"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Auto-compact's controls (#342): a correction is saved and marked, a new
// chat starts from the summary, a chat can be kept verbatim, and the
// threshold setting takes only its three choices.
func TestAutoCompact(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg, err := config.NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{DB: db, Config: cfg, Conversations: repositories.NewConversationRepo(db.SQL), Muninn: muninn.NewStore(db.SQL),
		Settings: repositories.NewSettingsRepo(db.SQL)}
	ctx := context.Background()

	conv, _ := a.Conversations.Create(ctx, "Dog names", "general-assistant", "")
	if _, err := a.ConversationSummary(ctx, conv.ID); err == nil {
		t.Fatal("a summary before any")
	} else if code, _ := contracts.ErrorCode(err); code != "SUMMARY_NOT_FOUND" {
		t.Fatalf("no summary: %v", err)
	}
	if err := a.Muninn.SaveSummary(ctx, muninn.Summary{ConversationID: conv.ID, ThroughMessageID: "m1", MessageCount: 4, Text: "The dog is Max."}); err != nil {
		t.Fatal(err)
	}
	edited, err := a.EditConversationSummary(ctx, conv.ID, "  The dog is Rex.  ")
	if err != nil || edited.Text != "The dog is Rex." || !edited.Edited {
		t.Fatalf("edit: %+v %v", edited, err)
	}
	if _, err := a.EditConversationSummary(ctx, conv.ID, " "); err == nil {
		t.Fatal("an empty summary was saved")
	}

	next, err := a.ConversationFromSummary(ctx, conv.ID)
	if err != nil || next.Title != "Dog names (continued)" || next.ProfileID != "general-assistant" {
		t.Fatalf("from summary: %+v %v", next, err)
	}
	msgs, _ := a.Conversations.ListMessages(ctx, next.ID)
	if len(msgs) != 2 || !strings.Contains(msgs[0].Content, "The dog is Rex.") || msgs[1].Role != "assistant" {
		t.Fatalf("new chat starts with %+v", msgs)
	}

	if a.compactOff(ctx, conv.ID) {
		t.Fatal("verbatim from the start")
	}
	if err := a.Conversations.SetCompactOff(ctx, conv.ID, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.Conversations.Get(ctx, conv.ID); !got.CompactOff || !a.compactOff(ctx, conv.ID) {
		t.Fatalf("verbatim: %+v", got)
	}

	if a.compactAt(ctx) != 0.5 {
		t.Fatal("default threshold")
	}
	if err := a.applySettingsPatch(ctx, map[string]any{"compact_at": "late"}); err != nil || a.compactAt(ctx) != 0.7 {
		t.Fatalf("late: %v %v", err, a.compactAt(ctx))
	}
	if err := a.applySettingsPatch(ctx, map[string]any{"compact_at": "sometimes"}); err == nil {
		t.Fatal("a fourth choice was accepted")
	}
}
