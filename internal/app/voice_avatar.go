package app

import "slices"

// settingVoiceAvatar is the personal key for the character a person sees
// in voice chat (#1369).
const settingVoiceAvatar = "voice_avatar"

// VoiceAvatars are the built-in characters voice chat can show. They ship
// with the app as small vector drawings, so there is nothing to download.
// Keep this list in step with web/src/features/chat/avatars.ts.
var VoiceAvatars = []string{"ratatoskr", "pebble", "lumen"}

// validAvatar reports whether id is no character ("") or a built-in one.
func validAvatar(id string) bool {
	return id == "" || slices.Contains(VoiceAvatars, id)
}
