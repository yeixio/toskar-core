package simple

import (
	"context"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A spoken answer isn't asked for Markdown, which a voice reads out as
// symbols; a written one still is (#795).
func TestSpokenTurnIsNotAskedForMarkdown(t *testing.T) {
	for _, spoken := range []bool{false, true} {
		env := &scriptedEnv{replies: []string{"Heritage Coffee, on Front Street."}}
		ctx := context.Background()
		if spoken {
			ctx = turnopts.WithSpoken(ctx)
		}
		events, err := New().Run(ctx, contracts.Task{Prompt: "Where's a good cafe?"}, contracts.AIProfile{
			Roles: []contracts.ModelRole{{Role: "assistant", ModelID: "m"}},
		}, env)
		if err != nil {
			t.Fatal(err)
		}
		for range events {
		}
		if len(env.seen) == 0 {
			t.Fatal("the model wasn't asked")
		}
		sys := env.seen[0][0].Content
		if asked := strings.Contains(sys, "Markdown"); asked == spoken {
			t.Errorf("spoken %v: system prompt %q", spoken, sys)
		}
	}
}
