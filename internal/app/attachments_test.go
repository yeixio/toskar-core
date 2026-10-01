package app

import (
	"context"
	"strings"
	"testing"

	"github.com/yeixio/yggdrasil-core/internal/artifacts"
	"github.com/yeixio/yggdrasil-core/internal/profiles"
)

func TestAttachmentsReachTheTurnAsData(t *testing.T) {
	a, conv := memoryApp(t)
	ctx := context.Background()
	a.Artifacts = artifacts.NewStore(a.DB.SQL, t.TempDir())

	// Uploaded before the chat existed, then sent with a message.
	f, err := a.Artifacts.Save(ctx, artifacts.Input{Name: "warranty.md", Data: []byte("# Warranty\n\nTread wear is covered for 60,000 miles.")})
	if err != nil {
		t.Fatal(err)
	}
	attached, err := a.resolveAttachments(ctx, conv, []string{f.ID})
	if err != nil || len(attached) != 1 || attached[0].ConversationID != conv {
		t.Fatalf("resolve = %+v %v", attached, err)
	}
	env := &chatExecEnv{app: a, ctx: ctx, profile: profiles.Profile{}, conversationID: conv, trace: &turnTrace{}, attachments: attached}
	block := env.attachmentBlock(ctx, "Summarize this")
	if !strings.Contains(block, "File attached to this message by the user: warranty.md") || !strings.Contains(block, "60,000 miles") {
		t.Fatalf("block = %q", block)
	}
	meta := env.trace.meta()
	if meta == nil || meta.Steps[0].Text != "Read warranty.md" || meta.Sources[0].Kind != "file" || !env.trace.sawUntrusted() {
		t.Fatalf("meta = %+v", meta)
	}

	// On a later turn the file is still there, but adds only what matches.
	later := &chatExecEnv{app: a, ctx: ctx, profile: profiles.Profile{}, conversationID: conv, trace: &turnTrace{}}
	if got := later.attachmentBlock(ctx, "What does the warranty cover?"); !strings.Contains(got, "attached earlier in this chat") {
		t.Fatalf("later = %q", got)
	}

	// A file from another chat is refused.
	other, _ := a.Conversations.Create(ctx, "other", "", "")
	if _, err := a.resolveAttachments(ctx, other.ID, []string{f.ID}); err == nil {
		t.Fatal("a file from another chat was accepted")
	}
	if _, err := a.resolveAttachments(ctx, conv, []string{"missing"}); err == nil {
		t.Fatal("a missing file was accepted")
	}
}

func TestCreatedFilesAreListedWithTheAnswer(t *testing.T) {
	tr := &turnTrace{}
	tr.tool("files.create", map[string]any{"name": "Budget.xlsx"}, map[string]any{
		"id": "f1", "name": "Budget.xlsx", "kind": "spreadsheet", "mime_type": "application/x", "size_bytes": int64(5120),
	})
	meta := tr.meta()
	if meta == nil || len(meta.Files) != 1 || meta.Files[0].Producer != "assistant" || meta.Files[0].Size != 5120 || meta.Steps[0].Text != "Created Budget.xlsx" {
		t.Fatalf("meta = %+v", meta)
	}
	if !tr.hasSideEffects() {
		t.Fatal("a turn that created a file must not be retried")
	}
}
