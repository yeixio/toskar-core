package app

import (
	"context"
	"testing"

	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Settings → Tool permissions limit every profile (#1206): a profile's allow
// asks while the setting says ask, a setting can't loosen a profile, and
// git.push asks each time whatever either says.
func TestComputerToolRulesLimitProfiles(t *testing.T) {
	a, _ := memoryApp(t)
	ctx := context.Background()
	profile := profiles.Profile{Tools: []contracts.ToolPolicy{
		{ToolID: "filesystem.write", Policy: "allow"},
		{ToolID: "terminal", Policy: "deny"},
		{ToolID: "git.commit", Policy: "allow"},
		{ToolID: "git.push", Policy: "allow"},
		{ToolID: "filesystem.read", Policy: "allow"},
	}}
	policyOf := func(p profiles.Profile, id string) string { return tools.PolicyForProfile(p, id) }

	got := a.withComputerToolRules(ctx, profile)
	for id, want := range map[string]string{
		"filesystem.write": "ask",
		"terminal":         "deny",
		"git.commit":       "ask",
		"git.push":         "ask-each-time",
		"filesystem.read":  "allow",
	} {
		if p := policyOf(got, id); p != want {
			t.Errorf("default settings: %s = %q, want %q", id, p, want)
		}
	}
	if policyOf(profile, "filesystem.write") != "allow" {
		t.Fatal("the stored profile was changed")
	}

	for k, v := range map[string]string{"tool_file_writes": "allow", "tool_terminal": "allow", "tool_git": "allow"} {
		if err := a.Settings.Set(ctx, k, v); err != nil {
			t.Fatal(err)
		}
	}
	got = a.withComputerToolRules(ctx, profile)
	if p := policyOf(got, "filesystem.write"); p != "allow" {
		t.Errorf("allowed in Settings and the profile, got %q", p)
	}
	if p := policyOf(got, "terminal"); p != "deny" {
		t.Errorf("Settings loosened a profile's deny: %q", p)
	}
	if p := policyOf(got, "git.push"); p != "ask-each-time" {
		t.Errorf("git.push ran unasked: %q", p)
	}

	if err := a.Settings.Set(ctx, "tool_file_writes", "deny"); err != nil {
		t.Fatal(err)
	}
	if p := policyOf(a.withComputerToolRules(ctx, profile), "filesystem.write"); p != "deny" {
		t.Errorf("Settings deny did not apply: %q", p)
	}
}
