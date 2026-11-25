package app

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/yeixio/toskar-core/internal/features"
	"github.com/yeixio/toskar-core/internal/inventory"
	"github.com/yeixio/toskar-core/internal/speech"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// One-step setup (#1355): every feature declares what it needs, the same
// check says what is missing on this computer, and one confirmation
// installs all of it. When nothing that fits exists, it says what would.

// Feature and FeatureNeed are the check's answer, in package features.
type (
	Feature     = features.Feature
	FeatureNeed = features.Need
)

// featureNeeds is what each feature needs, in the order Settings lists them.
var featureNeeds = []struct {
	id    string
	needs []string
}{
	{"voice", []string{"voice", "chat"}},
	{"door", []string{"camera", "voice", "chat", "vision"}},
	{"pictures", []string{"images"}},
	{"clips", []string{"video"}},
	{"see", []string{"vision"}},
	{"meaning", []string{"embedding"}},
	{"automationTools", []string{"tools"}},
	{"train", []string{"trainer"}},
}

// features is every feature and what it still needs.
func (a *App) Features(ctx context.Context) []Feature {
	needs := a.needStates(ctx)
	out := make([]Feature, 0, len(featureNeeds))
	for _, f := range featureNeeds {
		feature := Feature{ID: f.id, Ready: true, Needs: []FeatureNeed{}}
		for _, id := range f.needs {
			n := needs[id]
			feature.Needs = append(feature.Needs, n)
			if !n.Ready {
				feature.Ready = false
				if n.Install != "" && !n.Installing {
					feature.SetupBytes += n.SizeBytes
				}
			}
			feature.Installing = feature.Installing || n.Installing
		}
		out = append(out, feature)
	}
	return out
}

// needStates works out each need once, since features share them.
func (a *App) needStates(ctx context.Context) map[string]FeatureNeed {
	snap := a.Capabilities(ctx)
	ability := map[string]inventory.Ability{}
	for _, ab := range snap.Abilities {
		ability[ab.ID] = ab
	}
	list := a.installedModels(ctx)
	downloading := map[string]bool{}
	chat, tools := false, false
	for _, m := range list {
		if m.Status == "downloading" {
			downloading[m.ID] = true
		}
		if m.Installed && contracts.SupportRoleOf(m) == "" {
			chat = true
			tools = tools || toolCallSupport(m.Capabilities) != "unsupported"
		}
	}
	model := func(need string, ready bool, offer contracts.Model, size int64) FeatureNeed {
		n := FeatureNeed{Need: need, Ready: ready}
		if ready || offer.ID == "" {
			return n
		}
		if size == 0 {
			size = int64(downloadSize(offer))
		}
		n.Install, n.Option, n.Name, n.SizeBytes = "model", offer.ID, firstNonEmpty(offer.DisplayName, offer.ID), size
		n.Installing = downloading[offer.ID]
		return n
	}

	out := map[string]FeatureNeed{}
	out["chat"] = model("chat", chat, a.chatOption(ctx, false), 0)
	out["tools"] = model("tools", tools, a.chatOption(ctx, true), 0)
	vision, size, _ := a.visionOption(ctx)
	out["vision"] = model("vision", ability["vision"].Available, vision, size)
	out["embedding"] = model("embedding", ability["meaning_search"].Available, a.embeddingOption(ctx, list), 0)
	out["voice"] = a.voiceNeed()
	out["images"] = a.mediaNeed(ctx, "images", ability["image_generation"])
	out["video"] = a.mediaNeed(ctx, "video", ability["video_generation"])
	out["trainer"] = FeatureNeed{Need: "trainer", Ready: ability["training"].Available}
	out["camera"] = FeatureNeed{Need: "camera", Ready: a.hasCamera(ctx)}
	return out
}

// chatOption is the chat model to offer: the recommended one that fits
// this computer, and with tools, one that can call them.
func (a *App) chatOption(ctx context.Context, tools bool) contracts.Model {
	if a.Models == nil {
		return contracts.Model{}
	}
	hw, _ := a.detectHardware(ctx)
	rec, err := a.Models.Recommend(ctx, "general", hw, a.communitySignals(ctx))
	if err != nil {
		return contracts.Model{}
	}
	for _, m := range rec.Models {
		if m.Installed || contracts.SupportRoleOf(m) != "" {
			continue
		}
		if tools && toolCallSupport(m.Capabilities) == "unsupported" {
			continue
		}
		return m
	}
	return contracts.Model{}
}

// embeddingOption is the smallest embedding model in the catalog that
// fits this computer, since it runs beside the chat model.
func (a *App) embeddingOption(ctx context.Context, list []contracts.Model) contracts.Model {
	total := a.memoryTotal(ctx)
	var options []contracts.Model
	for _, m := range list {
		if m.Installed || contracts.SupportRoleOf(m) != contracts.SupportEmbedding {
			continue
		}
		if total > 0 && m.MemoryNeeded > 0 && float64(m.MemoryNeeded) > float64(total)*0.25 {
			continue
		}
		options = append(options, m)
	}
	sort.SliceStable(options, func(i, j int) bool { return downloadSize(options[i]) < downloadSize(options[j]) })
	if len(options) == 0 {
		return contracts.Model{}
	}
	return options[0]
}

// voiceNeed is speaking and listening: Kitten, on this computer.
func (a *App) voiceNeed() FeatureNeed {
	n := FeatureNeed{Need: "voice"}
	if a.voiceWorker == nil {
		n.Detail = "speech isn't set up on this computer"
		return n
	}
	st := a.voiceWorker.KittenStatus()
	switch {
	case st.State == speech.KittenInstalled:
		n.Ready = true
	case st.Unavailable != "":
		n.Detail = st.Unavailable
	default:
		n.Install, n.Name = "voice", "Kitten TTS"
		n.Installing = st.State == speech.KittenInstalling
	}
	return n
}

// mediaNeed is pictures or clips: ready on any computer, or the best place
// to set them up, from the same offer chat makes (§29).
func (a *App) mediaNeed(ctx context.Context, kind string, ab inventory.Ability) FeatureNeed {
	n := FeatureNeed{Need: kind, Ready: ab.Available}
	if st := a.mediaSetup(kind); st != nil {
		status := st.Status()
		n.Ready = n.Ready || status.Ready
		if job := status.Job; job != nil && job.Running {
			n.Installing = true
			if job.Total > 0 {
				n.Percent = int(job.Done * 100 / job.Total)
			}
		}
		if !status.Supported {
			n.Detail = status.Unsupported
		}
	}
	if n.Ready {
		return n
	}
	if s := ab.Setup; s != nil {
		n.Install, n.Option, n.Name, n.SizeBytes = kind, s.Option, s.Name, s.SizeBytes
		if s.Remote {
			n.NodeID, n.NodeName = s.NodeID, s.NodeName
		}
		n.Detail = ""
	} else if n.Detail == "" {
		n.Detail = ab.Note
	}
	return n
}

// setUpFeature starts setting up everything a feature still needs that
// this computer, or a paired one, can install, and returns the feature.
func (a *App) SetUpFeature(ctx context.Context, id string) (Feature, error) {
	var feature *Feature
	for _, f := range a.Features(ctx) {
		if f.ID == id {
			feature = &f
			break
		}
	}
	if feature == nil {
		return Feature{}, features.ErrUnknown
	}
	var errs []error
	for _, n := range feature.Needs {
		if n.Ready || n.Installing || n.Install == "" {
			continue
		}
		switch n.Install {
		case "model":
			errs = append(errs, a.installModelOn(ctx, n.Option, "", false))
		case "voice":
			errs = append(errs, a.installKitten(ctx))
		case "images", "video":
			errs = append(errs, a.startMediaOn(ctx, n.Install, n.Option, n.NodeID))
		}
	}
	a.capCache.Delete("snapshot")
	for _, f := range a.Features(ctx) {
		if f.ID == id {
			return f, errors.Join(errs...)
		}
	}
	return *feature, errors.Join(errs...)
}

// startMediaOn starts a picture or clip setup here, or on a paired computer.
func (a *App) startMediaOn(ctx context.Context, kind, modelID, nodeID string) error {
	body, _ := json.Marshal(map[string]string{"model_id": modelID})
	status, raw, handled, err := a.RemoteMediaSetup(ctx, nodeID, kind, "POST", body)
	if handled {
		if err != nil {
			return err
		}
		if status >= 300 {
			var e struct {
				Error string `json:"error"`
			}
			_ = json.Unmarshal(raw, &e)
			return errors.New(firstNonEmpty(e.Error, "that computer couldn't start the setup"))
		}
		return nil
	}
	st := a.mediaSetup(kind)
	if st == nil {
		return errors.New("this computer doesn't make " + kind)
	}
	return st.Start(modelID)
}
