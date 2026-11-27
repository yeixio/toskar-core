package app

import (
	"context"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/profiles"
)

func TestMainFits(t *testing.T) {
	const gb = 1 << 30
	cases := []struct {
		need, total uint64
		want        bool
	}{
		{4 * gb, 16 * gb, true},
		{8 * gb, 16 * gb, true},
		{9 * gb, 16 * gb, false},
		{4 * gb, 0, false},
		{0, 16 * gb, false},
	}
	for _, c := range cases {
		if got := mainFits(c.need, c.total); got != c.want {
			t.Errorf("mainFits(%d, %d) = %v, want %v", c.need, c.total, got, c.want)
		}
	}
}

// Until someone chooses, Toskar keeps the main chat model ready while it
// fits; "always" keeps it running, and "managed" lets that go again (#1472).
func TestMainModelReadyChoice(t *testing.T) {
	a, _, _ := pictureApp(t, false)
	ctx := context.Background()
	a.mainModel.id, a.mainModel.at = "text-model", time.Now()
	a.memTotal.Store(16 << 30)

	if a.mainReadyChoice(ctx) != "" {
		t.Fatal("a choice before anyone chose")
	}
	if !a.keptAsMain(ctx, "text-model") {
		t.Fatal("the main chat model, which fits, isn't kept ready")
	}
	if a.keptAsMain(ctx, "see-model") {
		t.Fatal("another model is kept as the main one")
	}

	if err := a.applySettingsPatch(ctx, map[string]any{"main_model_ready": "always"}); err != nil {
		t.Fatal(err)
	}
	if a.mainReadyChoice(ctx) != "always" || !a.alwaysRunning(ctx, "text-model") {
		t.Fatal("always didn't keep the main chat model running")
	}
	if err := a.applySettingsPatch(ctx, map[string]any{"main_model_ready": "managed"}); err != nil {
		t.Fatal(err)
	}
	if a.mainReadyChoice(ctx) != "managed" || a.alwaysRunning(ctx, "text-model") {
		t.Fatal("managed didn't let the model always kept running go")
	}
	if !a.keptAsMain(ctx, "text-model") {
		t.Fatal("managed doesn't keep the main chat model ready while it fits")
	}
	if err := a.applySettingsPatch(ctx, map[string]any{"main_model_ready": "sometimes"}); err == nil {
		t.Fatal("an unknown choice was saved")
	}

	// A model someone keeps running on the Models page stays so.
	if _, err := a.setAlwaysRunning(ctx, "see-model", true); err != nil {
		t.Fatal(err)
	}
	if err := a.applySettingsPatch(ctx, map[string]any{"main_model_ready": "managed"}); err != nil {
		t.Fatal(err)
	}
	if !a.alwaysRunning(ctx, "see-model") {
		t.Fatal("managed let go of a model the person kept running")
	}
}

// Choosing the main chat model sets the Fast model of the profile new chats
// use, and "" gives the choice back to Auto (#1481).
func TestMainChatModelChoice(t *testing.T) {
	a, _, _ := pictureApp(t, false)
	ctx := context.Background()
	fast := func() string {
		p, err := a.Profiles.Get(ctx, a.defaultProfileID(ctx))
		if err != nil {
			t.Fatal(err)
		}
		return profiles.RoleModel(p, profiles.RoleFast)
	}
	if err := a.applySettingsPatch(ctx, map[string]any{"main_chat_model": "text-model"}); err != nil {
		t.Fatal(err)
	}
	if fast() != "text-model" || a.mainChatModelID(ctx) != "text-model" {
		t.Fatalf("fast = %q, main = %q", fast(), a.mainChatModelID(ctx))
	}
	if err := a.applySettingsPatch(ctx, map[string]any{"main_chat_model": "no-such-model"}); err == nil {
		t.Fatal("a model that isn't installed was chosen")
	}
	if err := a.applySettingsPatch(ctx, map[string]any{"main_chat_model": ""}); err != nil {
		t.Fatal(err)
	}
	if fast() != "" {
		t.Fatalf("Auto didn't clear the Fast model: %q", fast())
	}
}
