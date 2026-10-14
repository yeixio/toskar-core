package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/yeixio/toskar-core/internal/imagegen"
	"github.com/yeixio/toskar-core/internal/inventory"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/structured"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// setupOptions lists what Yggdrasil can install to add a missing ability,
// with its size and the computer it would run on (Gungnir §28–29): for each
// ability, the best place among this computer and the online paired
// computers that can set it up (#153). wait fetches paired computers'
// setups first; without it, what is known is used.
func (a *App) setupOptions(ctx context.Context, s inventory.Snapshot, wait bool) []inventory.Setup {
	local := inventory.Node{}
	names := map[string]string{}
	for _, n := range s.Nodes {
		if n.Local {
			local = n
		}
		names[n.ID] = n.Name
	}
	kinds := []struct {
		ability, kind string
		tools         []string
	}{
		{"image_generation", "images", []string{"image.generate", "image.edit"}},
		{"video_generation", "video", []string{"video.generate"}},
	}
	peers := a.peerMediaSetups(ctx, wait)
	var out []inventory.Setup
	for _, k := range kinds {
		var places []setupPlace
		if st := a.mediaSetup(k.kind); st != nil {
			places = append(places, setupPlace{status: MediaSetupStatus{Status: st.Status()}, nodeID: local.ID, nodeName: local.Name})
		}
		for id, byKind := range peers {
			if st := byKind[k.kind]; st != nil {
				places = append(places, setupPlace{status: *st, nodeID: id, nodeName: names[id], remote: true})
			}
		}
		if best, ok := bestSetupPlace(places); ok {
			m := best.model
			out = append(out, inventory.Setup{Ability: k.ability, Option: m.ID, Name: m.Name, SizeBytes: m.SizeBytes,
				NodeID: best.nodeID, NodeName: best.nodeName, Remote: best.remote, FreeBytes: best.status.FreeBytes,
				Tools: k.tools, Slow: !best.status.Accelerated, TightMemory: m.TightMemory || m.GraphicsShort,
				Verdict: m.Verdict, Short: m.Short, NeedBytes: m.NeedBytes, HaveBytes: m.HaveBytes})
		}
	}
	return out
}

// setupPlace is a computer an ability could be set up on.
type setupPlace struct {
	status   MediaSetupStatus
	nodeID   string
	nodeName string
	remote   bool
	model    imagegen.ModelStatus
	score    int
}

// bestSetupPlace picks where to set an ability up: a computer whose GPU
// would do the work first, then one with memory to spare, and this
// computer when they're equal, since nothing has to travel. A computer
// without the memory or the disk space for the recommended model isn't
// offered, and none is when the ability is already ready on one.
func bestSetupPlace(places []setupPlace) (setupPlace, bool) {
	var best setupPlace
	found := false
	for _, p := range places {
		if p.status.Ready {
			return setupPlace{}, false
		}
		if !p.status.Supported {
			continue
		}
		for _, m := range p.status.Models {
			if !m.Recommended || m.TooLittleMemory {
				continue
			}
			// Room for the download, with a gigabyte to spare.
			if p.status.FreeBytes > 0 && p.status.FreeBytes < uint64(m.SizeBytes)+1e9 {
				continue
			}
			p.model, p.score = m, 0
			if p.status.Accelerated {
				p.score += 100
			}
			if !m.TightMemory && !m.GraphicsShort {
				p.score += 10
			}
			if !p.remote {
				p.score += 5
			}
			if !found || p.score > best.score {
				best, found = p, true
			}
		}
	}
	return best, found
}

func setupOffer(a inventory.Ability, request string) *contracts.SetupOffer {
	s := a.Setup
	return &contracts.SetupOffer{Ability: a.ID, Label: a.Label, Option: s.Option, Name: s.Name, SizeBytes: s.SizeBytes,
		NodeID: s.NodeID, NodeName: s.NodeName, Request: request, Slow: s.Slow, TightMemory: s.TightMemory,
		Remote: s.Remote, FreeBytes: s.FreeBytes, Verdict: s.Verdict, Short: s.Short, NeedBytes: s.NeedBytes, HaveBytes: s.HaveBytes}
}

// offerSetup answers a request for an ability that is not installed but can
// be, such as "Make me an image of a Viking tree" before image generation is
// set up: it says what would be installed, how large it is, and where, and
// the app offers to install it and then finish the request (Gungnir §29).
// Only chats in the app get the offer; an API caller gets the model's
// answer as before.
func (a *App) offerSetup(ctx context.Context, profile profiles.Profile, conversationID, message string) (<-chan pluginapi.ChatChunk, bool) {
	if conversationID == "" || len(structured.SchemaFrom(ctx)) > 0 {
		return nil, false
	}
	snap := a.Capabilities(ctx)
	if _, ok := inventory.Wants(snap, message); !ok {
		return nil, false
	}
	// Paired computers are asked now, so the offer names the best place.
	snap.Setups = a.setupOptions(ctx, snap, true)
	snap.Abilities = inventory.Abilities(snap)
	need, ok := inventory.Needs(snap, message)
	if !ok {
		return nil, false
	}
	// A profile that keeps these tools out is not offered them.
	allowed := false
	for _, id := range need.Setup.Tools {
		if !strings.EqualFold(tools.PolicyForProfile(profile, id), tools.PolicyDeny) {
			allowed = true
		}
	}
	if !allowed {
		return nil, false
	}
	offer := setupOffer(need, message)
	where, it := "on this computer", "This computer"
	switch {
	case offer.Remote:
		where, it = "on "+offer.NodeName, offer.NodeName
	case offer.NodeName != "":
		where = "on this computer (" + offer.NodeName + ")"
	}
	label := strings.ToLower(need.Label[:1]) + need.Label[1:]
	reply := fmt.Sprintf("I can't %s yet, but I can set it up %s: %s, %.1f GB to download.",
		label, where, offer.Name, float64(offer.SizeBytes)/1e9)
	if offer.Remote {
		if !offer.Slow {
			reply += " Its GPU will do the work."
		}
		if offer.FreeBytes > 0 {
			reply += fmt.Sprintf(" %s has %.0f GB free.", offer.NodeName, float64(offer.FreeBytes)/1e9)
		}
		reply += fmt.Sprintf(" Set it up below, and I'll finish this on %s once it's ready.", offer.NodeName)
	} else {
		reply += " Set it up below, and I'll finish this once it's ready."
	}
	// Say before it's set up if it will be slow or may fail there.
	if offer.TightMemory {
		reply += " " + it + " has less memory than it's comfortable with, so it may be slow or fail."
	}
	if offer.Slow {
		if need.ID == "video_generation" {
			reply += " " + it + " has no GPU acceleration for it, so a clip can take most of an hour."
		} else {
			reply += " " + it + " has no GPU acceleration for it, so each picture takes a few minutes."
		}
	}
	meta := &contracts.MessageMeta{Setup: offer,
		Steps: []contracts.ActivityStep{{Kind: "share", Text: "Found a way to " + label}}}
	return a.replyDirectly(ctx, conversationID, message, reply, meta), true
}

// mediaSlow reports whether making a picture (kind image or edit) or a
// clip (video) here takes minutes: no GPU acceleration, or less memory
// than its model is comfortable with.
func (a *App) mediaSlow(kind string) bool {
	setup := a.Images
	if kind == "video" {
		setup = a.Video
	}
	if setup == nil {
		return false
	}
	if !setup.Accelerated() {
		return true
	}
	for _, m := range setup.Status().Models {
		if m.ID == setup.ActiveModel() && (m.TightMemory || m.GraphicsShort) {
			return true
		}
	}
	return false
}
