package app

import (
	"context"
	"strings"
	"testing"
)

// Regression: toskar-core#1346
// The model is told its own name, so "which model are you" isn't answered
// with another maker's.
func TestTurnInstructionsNameTheModel(t *testing.T) {
	a, _, _ := pictureApp(t, false)
	ctx := context.Background()
	env := &chatExecEnv{app: a, ctx: ctx, modelOverride: "text-model"}
	got := env.TurnInstructions(ctx, "Say what model you are.")
	if !strings.Contains(got, "You are the model Text Model, running in Toskar") {
		t.Fatalf("no model line in:\n%s", got)
	}
	if (&chatExecEnv{app: a, ctx: ctx}).modelLine(ctx) != "" {
		t.Fatal("a line without a model")
	}
}
