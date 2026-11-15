package app

import (
	"context"

	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// computerToolRules are Settings → Tool permissions, the rules for what the
// AI may do on this computer (#1206). Each is a limit on every profile: a
// profile can be stricter, never looser. They default to ask, and nothing
// read them before, so a profile's allow ran unasked while Settings said
// ask.
var computerToolRules = []struct {
	setting string
	tools   []string
}{
	{"tool_terminal", []string{"terminal"}},
	{"tool_file_writes", []string{"filesystem.write"}},
	{"tool_git", []string{"git.add", "git.commit", "git.push"}},
}

// withComputerToolRules applies Settings → Tool permissions to a turn's
// profile. git.push always asks, each time: it sends the person's code off
// the computer.
func (a *App) withComputerToolRules(ctx context.Context, profile profiles.Profile) profiles.Profile {
	rules := map[string]string{}
	for _, r := range computerToolRules {
		v := tools.PolicyAsk
		if a.Settings != nil {
			if got, err := a.Settings.GetString(ctx, r.setting, tools.PolicyAsk); err == nil && got != "" {
				v = got
			}
		}
		for _, id := range r.tools {
			rules[id] = v
		}
	}
	rules["git.push"] = tools.Stricter(rules["git.push"], tools.PolicyAskEachTime)
	out := profile
	out.Tools = make([]contracts.ToolPolicy, len(profile.Tools))
	for i, t := range profile.Tools {
		if r, ok := rules[tools.Canonical(t.ToolID)]; ok {
			t.Policy = tools.Stricter(t.Policy, r)
		}
		out.Tools[i] = t
	}
	return out
}
