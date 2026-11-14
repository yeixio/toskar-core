package simple

import (
	"context"
	"testing"

	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// An API caller's temperature and max_tokens (#70) go to the calls that
// write the answer; Deliberate's drafts keep their own settings.
func TestAnswerOptionsReachTheAnswer(t *testing.T) {
	env := newRoleEnv(map[string]string{"assistant": "Final answer: $26", "drafter:2": "Final answer: $26", "drafter:3": "Final answer: $26"})
	want := pluginapi.GenerateOptions{Temperature: pluginapi.GreedyTemperature, MaxTokens: 64}
	ctx := pluginapi.WithAnswerOptions(context.Background(), want)
	events, err := New().Run(ctx, contracts.Task{Prompt: muffins}, contracts.AIProfile{
		Roles:         []contracts.ModelRole{{Role: "assistant", ModelID: "m"}},
		Orchestration: always,
	}, env)
	if err != nil {
		t.Fatal(err)
	}
	for range events {
	}
	// The answer also thinks as long as the turn's effort (#1140).
	if got := env.opts["assistant"]; got.Temperature != want.Temperature || got.MaxTokens != want.MaxTokens || got.ReasoningEffort == "" {
		t.Errorf("the answer's options = %+v, want %+v and an effort", got, want)
	}
	if got := env.opts["drafter:2"]; got.Temperature != 0.6 {
		t.Errorf("draft 2's options = %+v, want its own temperature", got)
	}
}

// The answer thinks as long as the turn's effort says: briefly when it's
// fast, at length when it's thorough (#1140). An effort the caller named,
// such as the API's reasoning_effort, stays as it is.
func TestAnswerThinksForTheEffort(t *testing.T) {
	for _, tc := range []struct {
		effort huginn.Effort
		asked  string
		want   string
	}{
		{huginn.EffortFast, "", "low"},
		{huginn.EffortBalanced, "", "medium"},
		{huginn.EffortThorough, "", "high"},
		{huginn.EffortFast, "high", "high"},
	} {
		env := newRoleEnv(map[string]string{"assistant": "42"})
		ctx := huginn.WithEffort(context.Background(), tc.effort)
		if tc.asked != "" {
			ctx = pluginapi.WithAnswerOptions(ctx, pluginapi.GenerateOptions{ReasoningEffort: tc.asked})
		}
		events, err := New().Run(ctx, contracts.Task{Prompt: "What is 17 + 25?"}, contracts.AIProfile{
			Roles: []contracts.ModelRole{{Role: "assistant", ModelID: "m"}},
		}, env)
		if err != nil {
			t.Fatal(err)
		}
		for range events {
		}
		if got := env.opts["assistant"].ReasoningEffort; got != tc.want {
			t.Errorf("%s, asked %q: reasoning effort %q, want %q", tc.effort, tc.asked, got, tc.want)
		}
	}
}

func TestSamplingTemperature(t *testing.T) {
	for in, want := range map[float64]struct {
		t  float64
		ok bool
	}{0: {0, false}, 0.7: {0.7, true}, pluginapi.GreedyTemperature: {0, true}} {
		if got, ok := pluginapi.SamplingTemperature(in); got != want.t || ok != want.ok {
			t.Errorf("SamplingTemperature(%v) = %v, %v", in, got, ok)
		}
	}
}
