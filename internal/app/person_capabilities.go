package app

import (
	"context"
	"strings"

	"github.com/yeixio/toskar-core/internal/artifacts"
	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// What a person may use, in a chat (#589): their capabilities decide the
// model's tools, Knowledge, hand-picked models, and attachments. (The
// capability inventory, what this computer can do, is capabilities.go.)

// capabilityTools are the tools each capability gives; a tool is left out
// of a turn when its capability is off.
var capabilityTools = map[auth.Capability][]string{
	auth.CapWeb: {"internet.search", "internet.open", "places.search", "places.details", "maps.route", "maps.distance",
		"browser.open", "browser.extract", "browser.screenshot", "browser.close", "browser.click", "browser.type", "browser.download"},
	auth.CapMakePictures: {"image.generate", "image.edit"},
	auth.CapMakeVideo:    {"video.generate"},
	auth.CapVoice:        {"speech.transcribe", "speech.synthesize"},
	auth.CapFiles:        {"filesystem.search", "filesystem.read", "filesystem.write", "files.create", "spreadsheet.analyze"},
	auth.CapAutomations:  {"automations.schedule"},
}

// personCapabilities is what the request's person may use now, read
// again, as the context may carry an older copy of them. Without people,
// everything.
func (a *App) personCapabilities(ctx context.Context) auth.Capabilities {
	if a.People == nil {
		return auth.DefaultCapabilities(auth.RoleOwner)
	}
	who := auth.PrincipalFrom(ctx).Person
	if person, err := a.People.Get(ctx, who.ID); err == nil {
		who = person
	}
	caps, err := a.People.CapabilitiesOf(ctx, who)
	if err != nil {
		return auth.DefaultCapabilities(who.Role)
	}
	return caps
}

// withCapabilities leaves out of a profile the tools a person's turned-off
// capabilities give, and Knowledge when that's off. With tools that change
// things off, any tool that writes goes too, such as an MCP server's.
func withCapabilities(profile profiles.Profile, caps auth.Capabilities) profiles.Profile {
	blocked := map[string]bool{}
	for c, ids := range capabilityTools {
		if !caps.Allows(c) {
			for _, id := range ids {
				blocked[id] = true
			}
		}
	}
	out := profile
	out.Tools = make([]contracts.ToolPolicy, 0, len(profile.Tools))
	for _, t := range profile.Tools {
		id := tools.Canonical(t.ToolID)
		if blocked[id] {
			continue
		}
		if !caps.Allows(auth.CapChangingTools) {
			if def, ok := tools.Lookup(id); ok && def.Risk == tools.RiskWrite {
				continue
			}
		}
		out.Tools = append(out.Tools, t)
	}
	if !caps.Allows(auth.CapKnowledge) {
		out.KnowledgeSources = nil
	}
	return out
}

// mediaTurnedOff reports a message asking to make a picture or a clip that
// the person's capabilities turn off. With files attached it says nothing:
// "what's in this video" isn't asking to make one, and the tools are left
// out of the turn anyway.
func mediaTurnedOff(caps auth.Capabilities, message string, attached bool) (auth.Capability, bool) {
	if attached {
		return "", false
	}
	asked := huginn.ToolsFor(huginn.Chat, message, []string{"image.generate", "video.generate"})
	for _, id := range asked {
		switch {
		case id == "video.generate" && !caps.Allows(auth.CapMakeVideo):
			return auth.CapMakeVideo, true
		case id == "image.generate" && !caps.Allows(auth.CapMakePictures):
			return auth.CapMakePictures, true
		}
	}
	return "", false
}

// attachmentCapability is what attaching a file needs: pictures and
// videos for a model to look at, or files.
func attachmentCapability(f artifacts.Artifact) auth.Capability {
	if f.Kind == "image" || strings.HasPrefix(f.MimeType, "image/") || strings.HasPrefix(f.MimeType, "video/") {
		return auth.CapPicturesIn
	}
	return auth.CapFiles
}

// capabilityOff is the refusal for something turned off for the person,
// naming the Owner, who can turn it on.
func (a *App) capabilityOff(ctx context.Context, c auth.Capability) error {
	return contracts.NewError("CAPABILITY_OFF", map[string]any{"capability": string(c), "admin": a.ownerName(ctx)},
		errCapabilityOff(c))
}

type errCapabilityOff auth.Capability

func (e errCapabilityOff) Error() string {
	return string(e) + " is turned off for your account; ask an Admin to turn it on"
}

// ownerName is the Owner's name, to say who can turn something on.
func (a *App) ownerName(ctx context.Context) string {
	if a.People != nil {
		if p, err := a.People.Get(ctx, auth.OwnerID); err == nil {
			return p.Name
		}
	}
	return "Owner"
}
