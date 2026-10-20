package locale

import (
	"strings"
	"testing"
	"time"
)

func TestResolve(t *testing.T) {
	cases := map[string]string{
		"":             "en",
		"de":           "de",
		"de-AT":        "de",
		"de_CH":        "de",
		"pt-PT":        "pt-BR",
		"zh-TW":        "zh-Hant",
		"zh-HK":        "zh-Hant",
		"zh-CN":        "zh-Hans",
		"zh":           "zh-Hans",
		"en-XA":        "en",
		"ar-XB":        "en",
		"xx":           "en",
		"nonsense tag": "en",
	}
	for tag, want := range cases {
		if got := Resolve(tag); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", tag, got, want)
		}
	}
}

func TestT(t *testing.T) {
	if got := T("en", "common:nav.settings", nil); got != "Settings" {
		t.Fatalf("en nav.settings = %q", got)
	}
	if got := T("de", "common:nav.settings", nil); got != "Einstellungen" {
		t.Errorf("de nav.settings = %q", got)
	}
	// Placeholders, plural forms, and numbers in the language's format.
	if got := T("en", "chat:steps.correctedCode", map[string]any{"count": 1}); got != "Checked the code and fixed 1 error" {
		t.Errorf("en one = %q", got)
	}
	if got := T("en", "chat:steps.correctedCode", map[string]any{"count": 1234}); got != "Checked the code and fixed 1,234 errors" {
		t.Errorf("en other = %q", got)
	}
	if got := T("de", "common:subsystems.connected", map[string]any{"count": 1234}); !strings.Contains(got, "1.234") {
		t.Errorf("de other = %q, want 1.234", got)
	}
	// A key the catalog lacks shows itself.
	if got := T("en", "common:no.such.key", nil); got != "common:no.such.key" {
		t.Errorf("missing key = %q", got)
	}
}

func TestLanguageName(t *testing.T) {
	cases := map[[2]string]string{
		{"es", "de"}: "Spanisch",
		{"es", "en"}: "Spanish",
		{"ja", "fr"}: "japonais",
		{"es", ""}:   "Spanish",
	}
	for in, want := range cases {
		if got := LanguageName(in[0], in[1]); got != want {
			t.Errorf("LanguageName(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

// This computer's zone is an IANA name clients understand, never Go's
// "Local", which browsers refuse.
func TestSystemZone(t *testing.T) {
	t.Setenv("TZ", "America/Juneau")
	if got := SystemZone(); got != "America/Juneau" {
		t.Fatalf("with TZ: %q", got)
	}
	t.Setenv("TZ", "")
	got := SystemZone()
	if got == "" || got == "Local" {
		t.Fatalf("without TZ: %q", got)
	}
	if _, err := time.LoadLocation(got); err != nil {
		t.Fatalf("%q doesn't load: %v", got, err)
	}
	if ZoneName("Local") == "Local" || ZoneName("Europe/Berlin") != "Europe/Berlin" {
		t.Fatal("ZoneName")
	}
}
