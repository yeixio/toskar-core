package guide

import (
	"strings"
	"testing"
)

// A detailed page's body answers what no overview does (#1222): the
// Topics fields and the troubleshooting on the Profiles page are only in
// its blocks. Each answer has the words that answer it.
func TestAboutReadsDetailedPages(t *testing.T) {
	for q, want := range map[string]struct{ section, text string }{
		"What does Never discusses do in Topics?":                      {"Profiles & Orchestration", "Never discusses"},
		"What does Mark as on topic do?":                               {"Profiles & Orchestration", "Mark as on topic"},
		"Why does a tool still ask first though my profile allows it?": {"Profiles & Orchestration", "Settings → Tool permissions is stricter"},
	} {
		ps := About(q)
		found := false
		for _, p := range ps {
			if p.Section == want.section && strings.Contains(p.Text, want.text) {
				found = true
			}
		}
		if !found {
			t.Errorf("About(%q) = %v, want %s with %q", q, ps, want.section, want.text)
		}
	}
}

// A section's overview still comes first when it answers: the details are
// asked only when it doesn't, so a page's detail never pushes a section's
// own overview aside.
func TestOverviewBeforeDetails(t *testing.T) {
	for _, q := range []string{"are my chats private?", "what features do you have?"} {
		ps := About(q)
		if len(ps) == 0 {
			t.Fatalf("About(%q) found nothing", q)
		}
		loadOnce.Do(load)
		for _, d := range details.passages {
			if d.Text == ps[0].Text {
				t.Errorf("About(%q) led with a detail passage: %v", q, ps[0])
			}
		}
	}
}

// A question naming Toskar finds a section's overview easily; its page
// then adds the detail that answers (#1222). Asked in chat, "In Toskar,
// what does Never discusses do?" got the five tabs and an answer that
// there is no such option.
func TestDetailsFollowTheirOverview(t *testing.T) {
	ps := About("In Toskar, what does Never discusses do in a profile's Topics?")
	found := false
	for _, p := range ps {
		if strings.Contains(p.Text, "Never discusses: Subjects always off limits") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no Topics detail: %v", ps)
	}
}
