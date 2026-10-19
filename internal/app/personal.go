package app

import (
	"context"
	"encoding/json"
	"github.com/yeixio/toskar-core/internal/auth"

	"github.com/yeixio/toskar-core/internal/personal"
)

// personalKey is the setting that holds how the person likes answers.
const personalKey = "personalization"

// PersonalStyle returns how the person likes answers (spec §38).
func (a *App) PersonalStyle(ctx context.Context) (personal.Style, error) {
	raw := a.personalString(ctx, personalKey, "")
	if raw == "" {
		return personal.Style{}, nil
	}
	var s personal.Style
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return personal.Style{}, nil
	}
	return s, nil
}

// SetPersonalStyle checks and saves a style. It is kept apart from tool
// permissions, which it cannot change.
func (a *App) SetPersonalStyle(ctx context.Context, s personal.Style) (personal.Style, error) {
	s, err := s.Clean()
	if err != nil {
		return personal.Style{}, err
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return personal.Style{}, err
	}
	return s, a.setPersonal(ctx, personalKey, string(raw))
}

// Each person keeps their own App language, assistant language, and
// personalization (#206). The Owner's are the install's, which everyone
// else starts from until they choose their own, and which notifications
// and the desktop use.
var personalKeys = map[string]bool{
	"ui_locale":               true,
	"assistant_language_mode": true,
	"assistant_language":      true,
	personalKey:               true,
	// Whether answers may offer a better-fitting model (#707).
	"suggest_models": true,
}

// personalSettingKey is where ctx's person keeps key.
func personalSettingKey(ctx context.Context, key string) string {
	if id := auth.PersonID(ctx); personalKeys[key] && id != "" && id != auth.OwnerID {
		return key + "@" + id
	}
	return key
}

// personalString is ctx's person's value for key, or the Owner's while
// they haven't chosen one, or def. A choice of "" (such as the system
// language) is a choice too.
func (a *App) personalString(ctx context.Context, key, def string) string {
	if a.Settings == nil {
		return def
	}
	if own := personalSettingKey(ctx, key); own != key {
		if v, ok, err := a.Settings.Get(ctx, own); err == nil && ok {
			if v == "" {
				return def
			}
			return v
		}
	}
	v, _ := a.Settings.GetString(ctx, key, def)
	return v
}

// setPersonal saves ctx's person's value for key.
func (a *App) setPersonal(ctx context.Context, key, value string) error {
	return a.Settings.Set(ctx, personalSettingKey(ctx, key), value)
}

// personalBlock is the style as instructions for a turn, or "".
func (a *App) personalBlock(ctx context.Context) string {
	s, err := a.PersonalStyle(ctx)
	if err != nil {
		return ""
	}
	return s.Block()
}
