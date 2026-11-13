package mcp

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

// run_swarm runs a question swarm and answers with its answer and the
// coordinator's chat (#1135); a bad mode is refused before it runs.
func TestRunSwarmTool(t *testing.T) {
	var got [3]string
	s := NewServer(Backend{
		RunSwarm: func(_ context.Context, prompt, mode, profile string) (SwarmAnswer, error) {
			got = [3]string{prompt, mode, profile}
			if profile == "visitor" {
				return SwarmAnswer{}, errors.New("starting a swarm needs the member role")
			}
			return SwarmAnswer{Answer: "Both work.", ConversationID: "c1"}, nil
		},
	})
	srv := httptest.NewServer(s)
	defer srv.Close()
	ctx := context.Background()
	c, err := DialHTTP(ctx, srv.URL, HTTPOptions{}, Hooks{}, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	list, err := c.ListTools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	listed := false
	for _, tool := range list {
		listed = listed || tool.Name == "run_swarm"
	}
	if !listed {
		t.Fatalf("run_swarm isn't listed: %+v", list)
	}

	res, err := c.CallTool(ctx, "run_swarm", map[string]any{"prompt": "Compare A and B", "mode": "race", "profile": "coding"})
	if err != nil || res.IsError {
		t.Fatalf("run_swarm = %+v, %v", res, err)
	}
	text := res.Content[0].Text
	if !strings.HasPrefix(text, "Both work.") || !strings.Contains(text, "c1") || !strings.Contains(text, "/chat?c=c1") {
		t.Fatalf("answer = %q", text)
	}
	if got != [3]string{"Compare A and B", "race", "coding"} {
		t.Fatalf("ran with %q", got)
	}

	for _, args := range []map[string]any{
		{"prompt": "x", "mode": "parade"},
		{},
		{"prompt": "x", "profile": "visitor"},
	} {
		if res, err := c.CallTool(ctx, "run_swarm", args); err != nil || !res.IsError {
			t.Errorf("%v: %+v, %v", args, res, err)
		}
	}

	// Without swarms, there's no such tool.
	plain := httptest.NewServer(NewServer(Backend{}))
	defer plain.Close()
	c2, err := DialHTTP(ctx, plain.URL, HTTPOptions{}, Hooks{}, "test")
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	if _, err := c2.CallTool(ctx, "run_swarm", map[string]any{"prompt": "x"}); err == nil {
		t.Fatal("run_swarm ran without swarms")
	}
}
