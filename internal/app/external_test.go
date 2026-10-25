package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/api"
	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/mcp"
	"github.com/yeixio/toskar-core/internal/runtimes/external"
	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestExternalModelsAreListedButNeverOffline(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o-mini"}]}`))
	}))
	defer srv.Close()
	a := &App{External: external.New(external.Config{BaseURL: srv.URL})}
	list, err := a.externalModels(context.Background())
	if err != nil || len(list) != 1 || list[0].ID != "ext:gpt-4o-mini" || !list[0].Installed || list[0].Status != "external" {
		t.Fatalf("models = %+v, %v", list, err)
	}
	// Reused for a minute.
	if _, _ = a.externalModels(context.Background()); calls != 1 {
		t.Fatalf("listed %d times", calls)
	}
	// No server: nothing, and nothing asked.
	if list, err := (&App{External: external.New(external.Config{})}).externalModels(context.Background()); list != nil || err != nil {
		t.Fatalf("unset: %v %v", list, err)
	}

	online := contracts.AIProfile{Tools: []contracts.ToolPolicy{{ToolID: "internet.search", Policy: "ask"}}}
	offline := contracts.AIProfile{Tools: []contracts.ToolPolicy{{ToolID: "internet.search", Policy: "deny"}}}
	if err := externalAllowed(profileOffline(online), false); err != nil {
		t.Fatalf("online profile: %v", err)
	}
	if code, _ := contracts.ErrorCode(externalAllowed(profileOffline(offline), false)); code != "EXTERNAL_OFFLINE_PROFILE" {
		t.Fatalf("offline profile: %s", code)
	}
	if code, _ := contracts.ErrorCode(externalAllowed(profileOffline(online), true)); code != "EXTERNAL_LOCAL_ONLY" {
		t.Fatalf("local-only material: %s", code)
	}
	// A profile without web search at all is offline too.
	if externalAllowed(profileOffline(contracts.AIProfile{}), false) == nil {
		t.Fatal("a profile without web search sent work out")
	}
}

// A request that turns tools off, as toskarctl ask does for CI (#455),
// still reaches a model on the external server: turning tools off isn't
// asking to keep work on this computer, which only the profile says.
func TestExternalModelWithToolsOff(t *testing.T) {
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = w.Write([]byte(`{"data":[{"id":"echo"}]}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": "From the server."}}}})
		_, _ = w.Write([]byte("data: " + string(chunk) + "\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()
	a, err := New(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })
	ctx := context.Background()
	if _, err := a.SetExternalServer(ctx, api.ExternalServerInput{BaseURL: srv.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	stream, err := a.RunChat(turnopts.With(ctx, &turnopts.Options{Tools: []string{}}), "general-assistant", "", "Hi", false, "ext:echo", "")
	if err != nil {
		t.Fatal(err)
	}
	var answer, failed string
	for chunk := range stream {
		answer += chunk.Content
		if chunk.Error != "" {
			failed = chunk.Code + " " + chunk.Error
		}
	}
	if failed != "" || answer != "From the server." {
		t.Fatalf("answer %q, error %q", answer, failed)
	}
}

// A key for chat only (#455) can't use MCP, which reaches tools and
// knowledge.
func TestChatOnlyKeyCantUseMCP(t *testing.T) {
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	a, err := New(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.DB.Close() })
	ctx := context.Background()
	rec, secret, err := a.APIKeys.Create(ctx, "CI")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.APIKeys.SetPermissions(ctx, rec.ID, auth.CIAPIKeyPermissions()); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	var refused *mcp.RefusedError
	if _, err := a.mcpAuthorize(req); !errors.As(err, &refused) {
		t.Fatalf("a chat-only key on MCP: %v", err)
	}
}
