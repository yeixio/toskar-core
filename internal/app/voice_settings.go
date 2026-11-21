package app

import (
	"context"
	"errors"

	"github.com/yeixio/toskar-core/internal/connectors"
	"github.com/yeixio/toskar-core/internal/speech"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// The Voice settings (#1304): an Admin installs Kitten TTS 2 mini and
// chooses the voice each language is read aloud in, for live voice mode,
// read-aloud on the phone and in the car, and the doorbell.

// voiceSettingKey is the setting that keeps lang's chosen voice.
func voiceSettingKey(lang string) string { return "speech_voice_" + lang }

// chosenVoice is the voice an Admin chose for lang, "" for automatic.
func (a *App) chosenVoice(lang string) string {
	if a.Settings == nil {
		return ""
	}
	v, _ := a.Settings.GetString(context.Background(), voiceSettingKey(lang), "")
	return v
}

func (a *App) speechVoices(ctx context.Context) (speech.VoiceSettings, error) {
	if a.voiceWorker == nil {
		return speech.VoiceSettings{}, errors.New("speech isn't set up on this computer")
	}
	s := a.voiceWorker.Settings(a.chosenVoice)
	s.Needs = a.modelNeeds(ctx)
	return s, nil
}

// modelNeeds are the models voice mode and the doorbell need that nothing
// installed here covers, each with the one to install (#667): a chat model
// to answer with, and one that can see a camera's picture. Installing Kitten
// sets up speaking and listening; these are the rest of "ready".
func (a *App) modelNeeds(ctx context.Context) []speech.ModelNeed {
	needs := []speech.ModelNeed{}
	if a.Models == nil {
		return needs
	}
	chat := false
	for _, m := range a.installedModels(ctx) {
		if m.Installed && contracts.SupportRoleOf(m) == "" {
			chat = true
			break
		}
	}
	// Only someone with a camera needs a model that sees for the door (#1343).
	_, sees := a.seeingModel(ctx)
	vision := !sees && a.hasCamera(ctx)
	var offer contracts.Model
	if !chat {
		hw, _ := a.detectHardware(ctx)
		if rec, err := a.Models.Recommend(ctx, "general", hw, a.communitySignals(ctx)); err == nil {
			for _, m := range rec.Models {
				if !m.Installed && contracts.SupportRoleOf(m) == "" {
					offer = m
					break
				}
			}
		}
	}
	if !chat && vision {
		// One model that chats and sees covers both needs (#1343): the
		// chat model that fits, when it sees, or else the one that sees.
		both := offer
		if !a.seesPictures(both) {
			both = contracts.Model{}
			if m, _, ok := a.visionOption(ctx); ok && contracts.SupportRoleOf(m) == "" {
				both = m
			}
		}
		if both.ID != "" {
			return append(needs, speech.ModelNeed{Ability: "both", Model: both.ID, Name: both.DisplayName, SizeBytes: int64(downloadSize(both))})
		}
	}
	if !chat {
		need := speech.ModelNeed{Ability: "chat"}
		if offer.ID != "" {
			need.Model, need.Name, need.SizeBytes = offer.ID, offer.DisplayName, int64(downloadSize(offer))
		}
		needs = append(needs, need)
	}
	if vision {
		need := speech.ModelNeed{Ability: "vision"}
		if m, size, ok := a.visionOption(ctx); ok {
			need.Model, need.Name, need.SizeBytes = m.ID, m.DisplayName, size
		}
		needs = append(needs, need)
	}
	return needs
}

// hasCamera is whether a connected service has a camera: a Reolink
// camera, or one in Home Assistant, which is asked only when it's
// connected.
func (a *App) hasCamera(ctx context.Context) bool {
	if a.Connectors == nil {
		return false
	}
	list, err := a.Connectors.List(ctx)
	if err != nil {
		return false
	}
	ha := false
	for _, st := range list {
		switch {
		case st.ID == "reolink" && st.Connected:
			return true
		case st.ID == "homeassistant" && st.Connected:
			ha = true
		}
	}
	if !ha {
		return false
	}
	for _, d := range a.connectedDevices(ctx) {
		if d.Kind == connectors.KindCamera {
			return true
		}
	}
	return false
}

// seesPictures is whether m can look at a picture once installed: it says
// it sees and the catalog has its vision projector.
func (a *App) seesPictures(m contracts.Model) bool {
	if m.ID == "" || !m.Capabilities.Vision {
		return false
	}
	entry, ok := a.Models.Catalog().Get(m.ID)
	return ok && entry.Projector != nil
}

// downloadSize is everything installing m downloads.
func downloadSize(m contracts.Model) uint64 {
	return max(m.DownloadBytes, m.SizeBytes)
}

// setSpeechVoice chooses lang's voice; "" goes back to automatic.
func (a *App) setSpeechVoice(ctx context.Context, lang, voice string) error {
	if a.voiceWorker == nil {
		return errors.New("speech isn't set up on this computer")
	}
	if voice != "" && !a.voiceWorker.ValidVoice(lang, voice) {
		return contracts.Errorf("INVALID_VOICE", map[string]any{"language": lang, "voice": voice}, "%q isn't a voice for %s here", voice, lang)
	}
	return a.Settings.Set(ctx, voiceSettingKey(lang), voice)
}

func (a *App) installKitten(context.Context) error {
	if a.voiceWorker == nil {
		return errors.New("speech isn't set up on this computer")
	}
	return a.voiceWorker.InstallKitten()
}

func (a *App) removeKitten(context.Context) error {
	if a.voiceWorker == nil {
		return errors.New("speech isn't set up on this computer")
	}
	return a.voiceWorker.RemoveKitten()
}
