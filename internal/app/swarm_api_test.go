package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/api/openai"
	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/mcp"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/swarm"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// quietProfile is a profile without tools, so nothing looks anything up on
// the web, whose strategy isn't swarm: swarm:<profile> runs one anyway.
func quietProfile(t *testing.T, s *swarmApp) string {
	t.Helper()
	p, err := s.Profiles.Create(context.Background(), profiles.Profile{Name: "Quiet", OrchestratorID: "simple", Roles: []contracts.ModelRole{{Role: "assistant"}}})
	if err != nil {
		t.Fatal(err)
	}
	return p.ID
}

// swarm:<profile> without tools runs a full swarm on the request's last
// user message, in a new chat with its earlier messages, in a project named
// after the job, and answers with the coordinator's answer, its progress as
// reasoning (#1135).
func TestSwarmFromRawAPI(t *testing.T) {
	s := newSwarmApp(t)
	profile := quietProfile(t, s)
	h := &openai.Handler{Profiles: s.Profiles, Swarm: s.swarmTurn,
		Permissions: func(r *http.Request) (auth.APIKeyPermissions, context.Context, error) {
			return auth.DefaultAPIKeyPermissions(), r.Context(), nil
		}}
	body := `{"model":"swarm:` + profile + `","messages":[{"role":"user","content":"What are we comparing?"},{"role":"assistant","content":"Two processes."},{"role":"user","content":"Explain how photosynthesis works and how respiration works."}]}`
	rec := httptest.NewRecorder()
	h.HandleChatCompletions(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
	var got struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				Reasoning string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
		Toskar struct {
			ConversationID string `json:"conversation_id"`
		} `json:"toskar"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK || len(got.Choices) != 1 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	msg := got.Choices[0].Message
	if msg.Content != "Merged." || !strings.Contains(msg.Reasoning, "Agent 1: Prices") || !strings.Contains(msg.Reasoning, "Agent 2 finished") {
		t.Fatalf("answer = %+v", msg)
	}
	ctx := context.Background()
	conv, err := s.Conversations.Get(ctx, got.Toskar.ConversationID)
	if err != nil || conv.SwarmID == "" || conv.ProjectID == "" || conv.ProfileID != profile {
		t.Fatalf("the coordinator's chat = %+v, %v", conv, err)
	}
	msgs, _ := s.Conversations.ListMessages(ctx, conv.ID)
	if len(msgs) != 4 || msgs[0].Content != "What are we comparing?" || msgs[3].Content != "Merged." {
		t.Fatalf("the chat's messages = %+v", msgs)
	}
	rec2, err := s.Swarms.Get(ctx, conv.SwarmID)
	if err != nil || rec2.Status != swarm.StatusDone || len(rec2.Agents) != 2 {
		t.Fatalf("the swarm = %+v, %v", rec2, err)
	}

	// Starting one needs the member role.
	visitor := auth.WithPrincipal(ctx, auth.Principal{Person: auth.Person{ID: "vic", Role: auth.RoleVisitor}})
	if _, _, err := s.swarmTurn(visitor, profile, "Explain both."); err != ErrSwarmRole {
		t.Fatalf("a visitor: %v", err)
	}
}

// MCP's run_swarm runs a question swarm and returns the answer and the
// coordinator's chat (#1135).
func TestRunSwarmOverMCP(t *testing.T) {
	s := newSwarmApp(t)
	profile := quietProfile(t, s)
	srv := httptest.NewServer(mcp.NewServer(mcp.Backend{RunSwarm: s.mcpRunSwarm}))
	defer srv.Close()
	ctx := context.Background()
	c, err := mcp.DialHTTP(ctx, srv.URL, mcp.HTTPOptions{}, mcp.Hooks{}, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	res, err := c.CallTool(ctx, "run_swarm", map[string]any{"prompt": "Explain how photosynthesis works and how respiration works.", "profile": profile})
	if err != nil || res.IsError {
		t.Fatalf("run_swarm = %+v, %v", res, err)
	}
	text := res.Content[0].Text
	list, _ := s.Swarms.List(ctx, "")
	if len(list) != 1 || !strings.HasPrefix(text, "Merged.") || !strings.Contains(text, "/chat?c="+list[0].ConversationID) {
		t.Fatalf("answer = %q, swarms = %+v", text, list)
	}
	if list[0].Mode != swarm.ModeSplit || len(list[0].Agents) != 2 {
		t.Fatalf("swarm = %+v", list[0])
	}
}
