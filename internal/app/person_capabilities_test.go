package app

import (
	"testing"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A person's turned-off capabilities leave their tools, writing tools, and
// Knowledge out of a turn (#589), and asking for a picture or a clip that's
// off gets a plain answer.
func TestWithCapabilities(t *testing.T) {
	profile := profiles.Profile{KnowledgeSources: []string{"handbook"}}
	for _, id := range []string{"internet.search", "image.generate", "video.generate", "filesystem.write", "filesystem.read", "terminal", "git.status", "speech.synthesize"} {
		profile.Tools = append(profile.Tools, contracts.ToolPolicy{ToolID: id, Policy: "allow"})
	}
	ids := func(p profiles.Profile) map[string]bool {
		out := map[string]bool{}
		for _, t := range p.Tools {
			out[t.ToolID] = true
		}
		return out
	}

	member := withCapabilities(profile, auth.DefaultCapabilities(auth.RoleMember))
	got := ids(member)
	if got["terminal"] || got["filesystem.write"] || !got["git.status"] || !got["image.generate"] || len(member.KnowledgeSources) != 1 {
		t.Fatalf("member keeps %v, knowledge %v", got, member.KnowledgeSources)
	}

	visitor := withCapabilities(profile, auth.DefaultCapabilities(auth.RoleVisitor))
	got = ids(visitor)
	if got["image.generate"] || got["video.generate"] || got["filesystem.read"] || !got["internet.search"] || !got["speech.synthesize"] {
		t.Fatalf("visitor keeps %v", got)
	}

	noKnowledge := auth.DefaultCapabilities(auth.RoleMember)
	noKnowledge[auth.CapKnowledge] = false
	if p := withCapabilities(profile, noKnowledge); len(p.KnowledgeSources) != 0 {
		t.Fatalf("knowledge kept: %v", p.KnowledgeSources)
	}

	visitorCaps := auth.DefaultCapabilities(auth.RoleVisitor)
	for message, want := range map[string]auth.Capability{
		"draw me a fox in the snow":  auth.CapMakePictures,
		"make a video of a sunrise":  auth.CapMakeVideo,
		"what's the weather in Oslo": "",
	} {
		c, off := mediaTurnedOff(visitorCaps, message, false)
		if c != want || off != (want != "") {
			t.Errorf("%q: %q %v, want %q", message, c, off, want)
		}
	}
	if _, off := mediaTurnedOff(visitorCaps, "make a video of a sunrise", true); off {
		t.Error("refused with a file attached, where the tools are left out instead")
	}
	if _, off := mediaTurnedOff(auth.DefaultCapabilities(auth.RoleMember), "draw me a fox", false); off {
		t.Error("a Member refused pictures")
	}

	if attachmentCapability(artifacts.Artifact{MimeType: "image/png"}) != auth.CapPicturesIn ||
		attachmentCapability(artifacts.Artifact{MimeType: "video/mp4"}) != auth.CapPicturesIn ||
		attachmentCapability(artifacts.Artifact{MimeType: "application/pdf"}) != auth.CapFiles {
		t.Error("attachment capabilities")
	}
}
