package app

import "slices"

// settingVoiceAvatar is the personal key for the character a person sees
// in voice chat (#1369).
const settingVoiceAvatar = "voice_avatar"

// VoiceAvatars are the built-in characters voice chat can show. They ship
// with the app as small vector drawings, so there is nothing to download.
// Keep this list in step with web/src/features/chat/avatars.ts.
var VoiceAvatars = []string{"ratatoskr", "pebble", "lumen"}

// DefaultVoiceAvatar is the character everyone sees until they choose:
// Ratatoskr, Toskar's own mascot. NoVoiceAvatar is the choice of none, the
// plain circle; "" from a client means the same.
const (
	DefaultVoiceAvatar = "ratatoskr"
	NoVoiceAvatar      = "none"
)

// validAvatar reports whether id is no character ("" or "none") or a
// built-in one.
func validAvatar(id string) bool {
	return id == "" || id == NoVoiceAvatar || slices.Contains(VoiceAvatars, id)
}

// voiceAvatarOf is what a saved choice shows: the default until the person
// chooses, and none once they turn it off.
func voiceAvatarOf(saved string) string {
	if saved == "" || !validAvatar(saved) {
		return DefaultVoiceAvatar
	}
	return saved
}
