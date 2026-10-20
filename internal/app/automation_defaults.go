package app

import (
	"context"
	"fmt"
	"time"

	"github.com/yeixio/toskar-core/internal/automations"
	"github.com/yeixio/toskar-core/internal/huginn"
)

// defaultProfileID is the profile an automation runs with when none is
// given, as the web form picks.
const defaultProfileID = "general-assistant"

// withCreateDefaults fills what an automation made through the API or
// toskarctl may leave out, so it isn't saved only to fail every run (#762):
// the profile the web form picks (General Assistant, else the first), Auto
// for the model, and this computer's time zone. A profile that doesn't
// exist is refused.
func withCreateDefaults(in automations.CreateInput, profileIDs []string, zone string) (automations.CreateInput, error) {
	if in.ProfileID == "" {
		for _, id := range profileIDs {
			if id == defaultProfileID {
				in.ProfileID = id
			}
		}
		if in.ProfileID == "" && len(profileIDs) > 0 {
			in.ProfileID = profileIDs[0]
		}
		if in.ProfileID == "" {
			return in, fmt.Errorf("there's no profile to run it with")
		}
	} else {
		known := false
		for _, id := range profileIDs {
			known = known || id == in.ProfileID
		}
		if !known {
			return in, fmt.Errorf("profile %q doesn't exist", in.ProfileID)
		}
	}
	if in.ModelID == "" {
		in.ModelID = huginn.AutoModelID
	}
	if in.Schedule.TimeZone == "" {
		in.Schedule.TimeZone = zone
	}
	return in, nil
}

// createDefaults is withCreateDefaults with this computer's profiles and time zone.
func (a *App) createDefaults(ctx context.Context, in automations.CreateInput) (automations.CreateInput, error) {
	var ids []string
	if a.Profiles != nil {
		list, err := a.Profiles.List(ctx)
		if err != nil {
			return in, err
		}
		for _, p := range list {
			ids = append(ids, p.ID)
		}
	}
	return withCreateDefaults(in, ids, time.Now().Location().String())
}
