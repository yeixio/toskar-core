package app

import (
	"context"
	"testing"
)

func TestValidLocale(t *testing.T) {
	for _, ok := range []string{"", "en", "es-MX", "zh-Hant-TW", "en-XA", "fil"} {
		if !validLocale(ok) {
			t.Errorf("%q should be accepted", ok)
		}
	}
	for _, bad := range []string{"e", "english!", "es_MX", "es-", "-es", "x-very-very-long-subtag-here", "<script>"} {
		if validLocale(bad) {
			t.Errorf("%q should be refused", bad)
		}
	}
}

// The app language is kept by the daemon, so every app on every device reads
// the same choice; "" means each device follows its system language.
func TestUILocaleSettingRoundTrips(t *testing.T) {
	t.Setenv("YGGDRASIL_DISCOVERY_ENABLED", "false")
	application, err := New(Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Shutdown(context.Background()) })
	ctx := context.Background()

	view, err := application.settingsView(ctx)
	if err != nil || view.UILocale != "" {
		t.Fatalf("default %q, %v", view.UILocale, err)
	}
	if err := application.applySettingsPatch(ctx, map[string]any{"ui_locale": "es-MX"}); err != nil {
		t.Fatal(err)
	}
	if view, _ = application.settingsView(ctx); view.UILocale != "es-MX" {
		t.Fatalf("got %q", view.UILocale)
	}
	if err := application.applySettingsPatch(ctx, map[string]any{"ui_locale": "español"}); err == nil {
		t.Fatal("an invalid tag should be refused")
	}
	if view, _ = application.settingsView(ctx); view.UILocale != "es-MX" {
		t.Fatalf("a refused value changed the setting to %q", view.UILocale)
	}
	if err := application.applySettingsPatch(ctx, map[string]any{"ui_locale": ""}); err != nil {
		t.Fatal(err)
	}
	if view, _ = application.settingsView(ctx); view.UILocale != "" {
		t.Fatalf("back to system: got %q", view.UILocale)
	}
}
