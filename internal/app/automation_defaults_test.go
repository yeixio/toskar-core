package app

import (
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/huginn"
)

// An automation made through the API with only a name, prompt, and schedule
// runs: it gets the web form's profile, Auto, and this computer's time zone,
// instead of being saved to fail every run (#762).
func TestCreateDefaults(t *testing.T) {
	bare := automations.CreateInput{Name: "Watch", Prompt: "p", Schedule: automations.Schedule{Kind: automations.KindInterval, EverySeconds: 60}}
	got, err := withCreateDefaults(bare, []string{"research", "general-assistant"}, "America/Juneau")
	if err != nil || got.ProfileID != "general-assistant" || got.ModelID != huginn.AutoModelID || got.Schedule.TimeZone != "America/Juneau" {
		t.Fatalf("defaults = %+v, %v", got, err)
	}
	if err := got.Schedule.Validate(); err != nil {
		t.Fatalf("an every-60-seconds schedule with the computer's zone: %v", err)
	}

	// Without General Assistant, the first profile; given values stay.
	given := bare
	given.ModelID, given.Schedule.TimeZone = "qwen", "Europe/Berlin"
	got, _ = withCreateDefaults(given, []string{"research"}, "America/Juneau")
	if got.ProfileID != "research" || got.ModelID != "qwen" || got.Schedule.TimeZone != "Europe/Berlin" {
		t.Fatalf("kept = %+v", got)
	}

	// A profile that doesn't exist, or none at all, is refused when saved.
	unknown := bare
	unknown.ProfileID = "gone"
	if _, err := withCreateDefaults(unknown, []string{"research"}, "UTC"); err == nil || !strings.Contains(err.Error(), "doesn't exist") {
		t.Fatalf("unknown profile: %v", err)
	}
	if _, err := withCreateDefaults(bare, nil, "UTC"); err == nil {
		t.Fatal("saved with no profile to run it")
	}
}
