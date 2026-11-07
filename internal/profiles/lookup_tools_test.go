package profiles

import "testing"

// The General assistant reads the AI news and the person's automation
// results in any chat (toskar-apps#142); an offline profile keeps the
// results, which stay on this computer, but not the news, which is read
// from toskar.ai.
func TestPresetsHaveLookupTools(t *testing.T) {
	policy := func(id, tool string) string {
		for _, p := range BuiltInPresets() {
			if p.ID != id {
				continue
			}
			for _, tp := range p.Tools {
				if tp.ToolID == tool {
					return tp.Policy
				}
			}
		}
		return ""
	}
	for _, id := range []string{PresetGeneral, PresetProgramming, PresetResearch} {
		if policy(id, "news.read") != "allow" || policy(id, "automations.results") != "allow" {
			t.Fatalf("%s: news.read %q, automations.results %q", id, policy(id, "news.read"), policy(id, "automations.results"))
		}
	}
	if policy(PresetCustom, "automations.results") != "allow" || policy(PresetCustom, "news.read") != "deny" {
		t.Fatalf("offline: news.read %q, automations.results %q", policy(PresetCustom, "news.read"), policy(PresetCustom, "automations.results"))
	}
}
