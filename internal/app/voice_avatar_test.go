package app

import (
	"context"
	"testing"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Each person picks the character they see in voice chat, none until they
// do, and only a built-in one (#1369).
func TestVoiceAvatarIsEachPersonsOwn(t *testing.T) {
	a, _, _ := pictureApp(t, false)
	owner := context.Background()
	sam := auth.WithPrincipal(owner, auth.Principal{Person: auth.Person{ID: "sam", Role: auth.RoleMember}})

	if s, err := a.settingsView(owner); err != nil || s.VoiceAvatar != "" {
		t.Fatalf("before a pick: %q, %v", s.VoiceAvatar, err)
	}
	if err := a.applySettingsPatch(sam, map[string]any{"voice_avatar": "pebble"}); err != nil {
		t.Fatal(err)
	}
	if s, _ := a.settingsView(sam); s.VoiceAvatar != "pebble" {
		t.Fatalf("Sam's: %q", s.VoiceAvatar)
	}
	if s, _ := a.settingsView(owner); s.VoiceAvatar != "" {
		t.Fatalf("Sam's pick is the Owner's too: %q", s.VoiceAvatar)
	}
	err := a.applySettingsPatch(sam, map[string]any{"voice_avatar": "someone-famous"})
	if code, _ := contracts.ErrorCode(err); code != "INVALID_SETTING" {
		t.Fatalf("an unknown character: %v", err)
	}
	if err := a.applySettingsPatch(sam, map[string]any{"voice_avatar": ""}); err != nil {
		t.Fatal(err)
	}
	if s, _ := a.settingsView(sam); s.VoiceAvatar != "" {
		t.Fatalf("after Off: %q", s.VoiceAvatar)
	}
}
