package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/gjallarhorn"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestUnapprovedToolsAreSkippedAndReported(t *testing.T) {
	a, _ := memoryApp(t)
	a.Notifications = gjallarhorn.NewHub(a.DB.SQL, a.Bus)
	a.Tools = tools.NewRegistry(t.TempDir(), a.Bus)
	ctx := context.Background()
	profile := profiles.Profile{Tools: []contracts.ToolPolicy{
		{ToolID: "terminal", Policy: "ask"},
		{ToolID: "filesystem.write", Policy: "allow"},
	}}
	env := &automationEnv{base: &chatExecEnv{app: a, ctx: ctx, profile: profile, trace: &turnTrace{}}, granted: []string{"filesystem.read"}}

	_, err := env.ExecuteTool(ctx, "terminal", map[string]any{"command": "rm -rf /tmp/x"})
	if err == nil || !strings.Contains(err.Error(), "skipped") {
		t.Fatalf("an unapproved tool must be skipped, got %v", err)
	}
	_, _ = env.ExecuteTool(ctx, "terminal", map[string]any{"command": "ls"})
	if got := env.skippedTools(); len(got) != 1 || got[0] != "terminal" {
		t.Fatalf("skipped = %v", got)
	}

	automationExecutor{app: a}.reportSkipped(ctx, automations.Automation{ID: "a1", Name: "Nightly cleanup"}, env.skippedTools())
	list, _, _ := a.Notifications.List(ctx, false, 0)
	if len(list) != 1 || list[0].Category != gjallarhorn.CategoryApproval || list[0].Title != "Nightly cleanup skipped Terminal" || list[0].Link != "/automations?id=a1" {
		t.Fatalf("notification = %+v", list)
	}
	if !errors.Is(func() error { _, e := tools.UnattendedPolicy(profile, nil, "terminal"); return e }(), tools.ErrNeedsApproval) {
		t.Fatal("policy")
	}
}

// A run a trigger started read someone else's words, so a tool that changes
// things outside Toskar is skipped even when the automation approved it
// (#204). Without a trigger, the approval stands.
func TestTriggerRunsSkipToolsThatChangeThings(t *testing.T) {
	a, _ := memoryApp(t)
	a.Tools = tools.NewRegistry(t.TempDir(), a.Bus)
	ctx := context.Background()
	profile := profiles.Profile{Tools: []contracts.ToolPolicy{{ToolID: "terminal", Policy: "allow"}}}
	newEnv := func(fromTrigger bool) *automationEnv {
		return &automationEnv{base: &chatExecEnv{app: a, ctx: ctx, profile: profile, trace: &turnTrace{}}, granted: []string{"terminal"}, fromTrigger: fromTrigger}
	}
	triggered := newEnv(true)
	if _, err := triggered.ExecuteTool(ctx, "terminal", map[string]any{"command": "echo hi"}); err == nil || !strings.Contains(err.Error(), "skipped") {
		t.Fatalf("approved terminal in a triggered run: %v", err)
	}
	if got := triggered.skippedTools(); len(got) != 1 || got[0] != "terminal" {
		t.Fatalf("skipped = %v", got)
	}
	if _, err := newEnv(false).ExecuteTool(ctx, "terminal", map[string]any{"command": "echo hi"}); err != nil && strings.Contains(err.Error(), "skipped") {
		t.Fatalf("approved terminal in a scheduled run was skipped: %v", err)
	}
}

// fakeCall records the calls a run makes to a tool that changes things.
type fakeCall struct{ calls []map[string]any }

func (f *fakeCall) ID() string          { return "homeassistant.call" }
func (f *fakeCall) DisplayName() string { return "Control Home Assistant" }
func (f *fakeCall) Description() string { return "fake" }
func (f *fakeCall) Execute(_ context.Context, args map[string]any) (map[string]any, error) {
	f.calls = append(f.calls, args)
	return map[string]any{"ok": true}, nil
}

// An automation's actions run on their own, even in a run a trigger
// started; any other call of the same tool is still skipped (#586).
func TestTriggerRunsTakeOnlyTheirActions(t *testing.T) {
	a, _ := memoryApp(t)
	a.Tools = tools.NewRegistry(t.TempDir(), a.Bus)
	tools.SetConnected("homeassistant", []tools.Definition{{ID: "homeassistant.call", Name: "Control Home Assistant", Source: "connector:homeassistant", Risk: tools.RiskWrite, DefaultPolicy: tools.PolicyAsk}})
	t.Cleanup(func() { tools.SetConnected("homeassistant", nil) })
	fake := &fakeCall{}
	a.Tools.Register(fake)
	ctx := context.Background()
	profile := profiles.Profile{Tools: []contracts.ToolPolicy{{ToolID: "homeassistant.call", Policy: "ask"}}}
	porch := []automations.Action{{Tool: "homeassistant.call", Args: map[string]string{"service": "light.turn_on", "entity_id": "light.porch"}}}
	env := &automationEnv{base: &chatExecEnv{app: a, ctx: ctx, profile: profile, trace: &turnTrace{}}, actions: porch, fromTrigger: true}

	// The model writes the call either way.
	for _, args := range []map[string]any{
		{"domain": "light", "service": "turn_on", "entity_id": "light.porch"},
		{"service": "light.turn_on", "entity_id": "Light.Porch"},
	} {
		if _, err := env.ExecuteTool(ctx, "homeassistant.call", args); err != nil {
			t.Fatalf("allowed action %v: %v", args, err)
		}
	}
	for _, args := range []map[string]any{
		{"domain": "lock", "service": "unlock", "entity_id": "lock.front_door"},
		{"domain": "light", "service": "turn_on", "entity_id": "light.porch, light.kitchen"},
		{"domain": "light", "service": "turn_off", "entity_id": "light.porch"},
	} {
		if _, err := env.ExecuteTool(ctx, "homeassistant.call", args); err == nil || !strings.Contains(err.Error(), "skipped") {
			t.Fatalf("call outside the actions %v: %v", args, err)
		}
	}
	if len(fake.calls) != 2 {
		t.Fatalf("calls = %v", fake.calls)
	}
	if got := env.actedCalls(); len(got) != 2 || got[0] != "homeassistant.call with domain light, entity_id light.porch, service turn_on" {
		t.Fatalf("acted = %v", got)
	}
	if got := env.skippedTools(); len(got) != 1 {
		t.Fatalf("skipped = %v", got)
	}
	// Without a trigger too, a tool the automation has only for its actions
	// runs only as one of them.
	scheduled := &automationEnv{base: &chatExecEnv{app: a, ctx: ctx, profile: profile, trace: &turnTrace{}}, actions: porch}
	if _, err := scheduled.ExecuteTool(ctx, "homeassistant.call", map[string]any{"domain": "lock", "service": "unlock", "entity_id": "lock.front_door"}); err == nil {
		t.Fatal("unlocked the door")
	}
	// A profile that denies the tool wins over the actions.
	denied := &automationEnv{base: &chatExecEnv{app: a, ctx: ctx, profile: profiles.Profile{Tools: []contracts.ToolPolicy{{ToolID: "homeassistant.call", Policy: "deny"}}}, trace: &turnTrace{}}, actions: porch}
	if _, err := denied.ExecuteTool(ctx, "homeassistant.call", map[string]any{"service": "light.turn_on", "entity_id": "light.porch"}); err == nil {
		t.Fatal("ran a tool the profile denies")
	}
	if !strings.Contains(actionsNote(porch), "service light.turn_on") {
		t.Fatalf("note = %q", actionsNote(porch))
	}
}
