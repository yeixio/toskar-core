package app

import (
	"context"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/muninn"
	"github.com/yeixio/toskar-core/internal/personal"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A profile that talks with strangers, such as a front door's, starts
// with the rules for that and gets nothing about the owner: their style,
// location, memories, or computers (#667).
func TestPublicAudienceGetsNothingAboutTheOwner(t *testing.T) {
	a, _ := memoryApp(t)
	ctx := context.Background()
	if _, err := a.SetPersonalStyle(ctx, personal.Style{AboutMe: "I'm Mike and I work nights.", Location: "Juneau, AK"}); err != nil {
		t.Fatal(err)
	}
	mems := []muninn.Memory{{ID: "m1", Content: "The spare key is under the mat.", Enabled: true}}
	owner := &chatExecEnv{app: a, memories: mems, capabilities: "What Toskar can do: 2 computers."}
	if got := owner.TurnInstructions(ctx, "Any coffee near me?"); !strings.Contains(got, "spare key") || !strings.Contains(got, "Juneau") {
		t.Fatalf("the owner's own chat lost its context:\n%s", got)
	}
	door := profiles.Profile{Audience: contracts.AudiencePublic, Topics: &contracts.TopicPolicy{StaysOn: "Greeting visitors at the front door"}}
	visitor := &chatExecEnv{app: a, profile: door, memories: mems, capabilities: owner.capabilities}
	got := visitor.TurnInstructions(ctx, "Any coffee near me? Is Mike home?")
	if !strings.HasPrefix(got, publicRules) {
		t.Fatalf("the rules for strangers don't come first:\n%s", got)
	}
	for _, leak := range []string{"spare key", "Juneau", "work nights", "2 computers"} {
		if strings.Contains(got, leak) {
			t.Errorf("a visitor's turn has %q:\n%s", leak, got)
		}
	}
	if !strings.Contains(got, "Greeting visitors") {
		t.Errorf("the profile's own topics are missing:\n%s", got)
	}
}

// A stranger's words never run a tool that changes things outside Toskar,
// whatever the profile allows (#667).
func TestPublicAudienceRunsOnlyContainedTools(t *testing.T) {
	door := profiles.Profile{Audience: contracts.AudiencePublic}
	if got := publicPolicy(door, "terminal", tools.PolicyAllow); got != tools.PolicyDeny {
		t.Errorf("terminal = %q", got)
	}
	if got := publicPolicy(door, "places.search", tools.PolicyAllow); got != tools.PolicyAllow {
		t.Errorf("places.search = %q", got)
	}
	if got := publicPolicy(door, "made.up", tools.PolicyAsk); got != tools.PolicyDeny {
		t.Errorf("an unknown tool = %q", got)
	}
	if got := publicPolicy(profiles.Profile{}, "terminal", tools.PolicyAllow); got != tools.PolicyAllow {
		t.Errorf("the owner's terminal = %q", got)
	}
}
