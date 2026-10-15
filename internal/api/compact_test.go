package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/muninn"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Auto-compact's routes (#342): their answers, their errors' statuses, and
// keeping a chat verbatim through PATCH.
func TestAutoCompactRoutes(t *testing.T) {
	var off []bool
	srv := NewServer(Dependencies{
		Compact: &Compacter{
			Summary: func(_ context.Context, id string) (muninn.Summary, error) {
				if id == "new" {
					return muninn.Summary{}, contracts.NewError("SUMMARY_NOT_FOUND", nil, errors.New("not yet"))
				}
				return muninn.Summary{ConversationID: id, Text: "Earlier: the dog is Rex."}, nil
			},
			Edit: func(_ context.Context, id, text string) (muninn.Summary, error) {
				return muninn.Summary{ConversationID: id, Text: text, Edited: true}, nil
			},
			Now: func(_ context.Context, id string) (muninn.Summary, error) {
				return muninn.Summary{}, contracts.NewError("NOTHING_TO_COMPACT", nil, errors.New("too short"))
			},
			NewChat: func(_ context.Context, id string) (contracts.Conversation, error) {
				return contracts.Conversation{ID: "c2", Title: "Dogs (continued)"}, nil
			},
			SetOff: func(_ context.Context, id string, v bool) error { off = append(off, v); return nil },
		},
		UpdateConversation: func(_ context.Context, id string, _, _, _ *string, _ *bool) (contracts.Conversation, error) {
			return contracts.Conversation{ID: id, CompactOff: len(off) > 0 && off[len(off)-1]}, nil
		},
	})
	call := func(method, path, body string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, localRequest(method, path, strings.NewReader(body)))
		out := map[string]any{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	if c, b := call(http.MethodGet, "/api/v1/conversations/c1/summary", ""); c != 200 || b["summary"] != "Earlier: the dog is Rex." {
		t.Fatalf("get: %d %v", c, b)
	}
	if c, _ := call(http.MethodGet, "/api/v1/conversations/new/summary", ""); c != http.StatusNotFound {
		t.Fatalf("none: %d", c)
	}
	if c, b := call(http.MethodPut, "/api/v1/conversations/c1/summary", `{"summary":"The dog is Rex."}`); c != 200 || b["edited"] != true {
		t.Fatalf("edit: %d %v", c, b)
	}
	if c, b := call(http.MethodPost, "/api/v1/conversations/c1/compact", ""); c != http.StatusBadRequest || b["error"].(map[string]any)["code"] != "NOTHING_TO_COMPACT" {
		t.Fatalf("compact: %d %v", c, b)
	}
	if c, b := call(http.MethodPost, "/api/v1/conversations/c1/from-summary", ""); c != http.StatusCreated || b["id"] != "c2" {
		t.Fatalf("from summary: %d %v", c, b)
	}
	if c, b := call(http.MethodPatch, "/api/v1/conversations/c1", `{"compact_off":true}`); c != 200 || b["compact_off"] != true || len(off) != 1 {
		t.Fatalf("verbatim: %d %v %v", c, b, off)
	}
}
