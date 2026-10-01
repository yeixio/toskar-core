package artifacts

import (
	"context"
	"strings"
	"testing"
)

func TestCreateToolMakesDownloads(t *testing.T) {
	s, _ := newStore(t)
	tool := &CreateTool{Store: s}
	ctx := WithConversation(context.Background(), "c1")

	res, err := tool.Execute(ctx, map[string]any{"name": "Budget.xlsx", "content": "month,amount\nJan,120\nFeb,95\n"})
	if err != nil {
		t.Fatal(err)
	}
	a, data, err := s.Read(ctx, res["id"].(string))
	if err != nil || a.Kind != "spreadsheet" || a.Producer != ProducerAssistant || a.ConversationID != "c1" || string(data[:2]) != "PK" {
		t.Fatalf("xlsx = %+v %v", a, err)
	}
	res, _ = tool.Execute(ctx, map[string]any{"name": "notes", "content": "# Plan"})
	if res["name"] != "notes.md" {
		t.Fatalf("a name without a type is saved as Markdown, got %v", res["name"])
	}
	res, _ = tool.Execute(ctx, map[string]any{"name": "run.exe", "content": "MZ"})
	if res["name"] != "run.exe.md" {
		t.Fatalf("an unknown type is not saved as itself, got %v", res["name"])
	}
	if _, err := tool.Execute(ctx, map[string]any{"name": "x.md", "content": "  "}); err == nil {
		t.Fatal("empty content")
	}
	if _, err := tool.Execute(ctx, map[string]any{"name": "x.md", "content": strings.Repeat("a", maxCreateBytes+1)}); err == nil {
		t.Fatal("too large")
	}
}
