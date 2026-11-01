package simple

import (
	"context"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// liveEnv writes each reply a word at a time, may show answers early, and can
// change the answer in its last check.
type liveEnv struct {
	*roleEnv
	early   bool
	checked string
}

func (e *liveEnv) MayShowEarly(context.Context) bool { return e.early }

func (e *liveEnv) CheckAnswer(_ context.Context, _, _, answer string) string {
	if e.checked != "" {
		return e.checked
	}
	return answer
}

func (e *liveEnv) Generate(ctx context.Context, role string, messages []pluginapi.ChatMessage) (<-chan pluginapi.ChatChunk, error) {
	whole, err := e.roleEnv.Generate(ctx, role, messages)
	if err != nil {
		return nil, err
	}
	first := <-whole
	words := strings.SplitAfter(first.Content, " ")
	ch := make(chan pluginapi.ChatChunk, len(words)+1)
	for _, word := range words {
		ch <- pluginapi.ChatChunk{Content: word}
	}
	ch <- pluginapi.ChatChunk{Done: true}
	close(ch)
	return ch, nil
}

// liveRun is what a client would see: each event's type and text.
func liveRun(t *testing.T, ctx context.Context, env *liveEnv, profile contracts.AIProfile, prompt string) []string {
	t.Helper()
	events, err := New().Run(ctx, contracts.Task{Prompt: prompt}, profile, env)
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	for evt := range events {
		switch evt.Type {
		case eventDelta, eventReplace, "agent.message":
			seen = append(seen, evt.Type+":"+evt.Content)
		}
	}
	return seen
}

func liveEnvWith(sequence ...string) *liveEnv {
	env := newRoleEnv(map[string]string{"assistant": sequence[0]})
	env.sequence["assistant"] = sequence[1:]
	return &liveEnv{roleEnv: env, early: true}
}

func TestLiveAnswer(t *testing.T) {
	assistant := []contracts.ModelRole{{Role: "assistant", ModelID: "m", Required: true}}
	profile := contracts.AIProfile{Roles: assistant, Tools: []contracts.ToolPolicy{{ToolID: "git.status", Policy: "allow"}}}
	live := turnopts.WithLive(context.Background())
	call := `{"tool_call":{"id":"git.status","args":{}}}`

	for _, c := range []struct {
		name  string
		ctx   context.Context
		env   *liveEnv
		want  []string
		state contracts.AIProfile
	}{
		{name: "shown as it's written", ctx: live, env: liveEnvWith("Juneau gets rain."), state: profile,
			want: []string{"agent.delta:Juneau", "agent.delta: gets", "agent.delta: rain."}},
		{name: "narration is taken back for a tool call", ctx: live, env: liveEnvWith("Let me check. "+call, "It's clean."), state: profile,
			want: []string{"agent.delta:Let", "agent.delta: me", "agent.delta: check.", "agent.replace:", "agent.delta:It's", "agent.delta: clean."}},
		{name: "a call alone shows nothing", ctx: live, env: liveEnvWith(call, "Clean."), state: profile,
			want: []string{"agent.delta:Clean."}},
		{name: "a text the last check changes is replaced", ctx: live, env: func() *liveEnv {
			e := liveEnvWith("Off the topic.")
			e.checked = "Back on it."
			return e
		}(), state: profile,
			want: []string{"agent.delta:Off", "agent.delta: the", "agent.delta: topic.", "agent.replace:Back on it."}},
		{name: "text held to the end still comes", ctx: live, env: liveEnvWith("An open brace {"), state: profile,
			want: []string{"agent.delta:An", "agent.delta: open", "agent.delta: brace", "agent.delta: {"}},
		{name: "not asked for", ctx: context.Background(), env: liveEnvWith("Juneau gets rain."), state: profile,
			want: []string{"agent.message:Juneau gets rain."}},
		{name: "an environment that keeps answers back", ctx: live, env: func() *liveEnv {
			e := liveEnvWith("Off the topic.")
			e.early, e.checked = false, "Set reply."
			return e
		}(), state: profile, want: []string{"agent.message:Set reply."}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := liveRun(t, c.ctx, c.env, c.state, "Check git status: is the repo clean?")
			if strings.Join(got, "\n") != strings.Join(c.want, "\n") {
				t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(c.want, "\n"))
			}
		})
	}
}

// A retry throws the first text away, and the person sees it go.
func TestLiveAnswerRetry(t *testing.T) {
	assistant := []contracts.ModelRole{{Role: "assistant", ModelID: "m", Required: true}}
	profile := contracts.AIProfile{Roles: assistant, Tools: []contracts.ToolPolicy{{ToolID: "git.status", Policy: "allow"}}}
	env := liveEnvWith("I would use the tool git.status to check that.", "It's clean.")
	got := liveRun(t, turnopts.WithLive(context.Background()), env, profile, "Check git status: is the repo clean?")
	if len(got) == 0 || got[len(got)-1] != "agent.delta: clean." {
		t.Fatalf("got %q", got)
	}
}

func TestLiveAnswerNotForDrafts(t *testing.T) {
	assistant := []contracts.ModelRole{{Role: "assistant", ModelID: "m", Required: true}}
	for name, profile := range map[string]contracts.AIProfile{
		"Deliberate": {Roles: assistant, Orchestration: contracts.OrchestrationPolicy{Deliberate: DeliberateAlways}},
		"Team":       {Roles: assistant, Orchestration: contracts.OrchestrationPolicy{Strategy: "team"}},
	} {
		t.Run(name, func(t *testing.T) {
			env := liveEnvWith("Final answer: 26")
			for _, seen := range liveRun(t, turnopts.WithLive(context.Background()), env, profile, "What is 13 times 2? Work it out.") {
				if strings.HasPrefix(seen, eventDelta) || strings.HasPrefix(seen, eventReplace) {
					t.Fatalf("showed %q", seen)
				}
			}
		})
	}
}
