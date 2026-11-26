package app

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// The fields PATCH /settings takes are the web app's SettingsPatch, no more
// and no fewer.
func TestSettingsPatchKeysMatchTheWebApp(t *testing.T) {
	src, err := os.ReadFile("../../web/src/types/api.ts")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	start := strings.Index(s, "export interface SettingsPatch {")
	if start < 0 {
		t.Fatal("SettingsPatch not found in web/src/types/api.ts")
	}
	end := strings.Index(s[start:], "\n}")
	web := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\s+([a-z_]+)\?:`).FindAllStringSubmatch(s[start:start+end], -1) {
		web[m[1]] = true
	}
	for k := range web {
		if !settingsPatchKeys[k] {
			t.Errorf("%s is in the web app's SettingsPatch but not in settingsPatchKeys", k)
		}
	}
	for k := range settingsPatchKeys {
		if !web[k] {
			t.Errorf("%s is in settingsPatchKeys but not in the web app's SettingsPatch", k)
		}
	}
}

// Every field applySettingsPatch reads by name is one it lets through.
func TestSettingsPatchReadsOnlyKnownKeys(t *testing.T) {
	src, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range regexp.MustCompile(`patch\["([a-z_]+)"\]`).FindAllStringSubmatch(string(src), -1) {
		if !settingsPatchKeys[m[1]] {
			t.Errorf("applySettingsPatch reads %s, which settingsPatchKeys refuses", m[1])
		}
	}
}

// A field that isn't a setting, such as the config file's name for one, is
// refused and named, not a 200 that changes nothing (#1464).
func TestUnknownSettingIsRefused(t *testing.T) {
	t.Setenv("YGGDRASIL_DISCOVERY_ENABLED", "false")
	application, err := New(Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Shutdown(context.Background()) })
	ctx := context.Background()

	err = application.applySettingsPatch(ctx, map[string]any{"advanced_mode": true, "ollama_port": true})
	if code, _ := contracts.ErrorCode(err); code != "INVALID_SETTING" || !strings.Contains(err.Error(), "ollama_port") {
		t.Fatalf("unknown field: %v (%q)", err, code)
	}
	view, err := application.settingsView(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if view.AdvancedMode {
		t.Fatal("a refused patch changed a setting")
	}
	if err := application.applySettingsPatch(ctx, map[string]any{"advanced_mode": true}); err != nil {
		t.Fatal(err)
	}
}
