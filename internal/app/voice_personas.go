package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/yeixio/toskar-core/internal/speech"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Voice personas (#1319): voices an Admin makes in the Voice settings, from
// the voices this computer has, each with a name, a language, and a speed.
// Everyone picks one for themselves, and it reads their answers in voice
// mode, Read aloud, the phone, the car, and the TV, and at the door an
// automation of theirs answers. Kitten TTS 2 mini's voices are the free
// ones; cloned voices and styles wait for the larger model.

// settingVoicePersonas keeps the personas, as JSON.
const settingVoicePersonas = "voice_personas"

// settingVoicePersona is the persona a person picked, "" for none.
const settingVoicePersona = "voice_persona"

// maxPersonas is the most personas an install keeps.
const maxPersonas = 50

// storedPersonas are the personas as saved.
func (a *App) storedPersonas(ctx context.Context) []speech.Persona {
	out := []speech.Persona{}
	if a.Settings == nil {
		return out
	}
	raw, _ := a.Settings.GetString(ctx, settingVoicePersonas, "")
	if raw == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

func (a *App) savePersonas(ctx context.Context, list []speech.Persona) error {
	b, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return a.Settings.Set(ctx, settingVoicePersonas, string(b))
}

// voicePersonas are the personas, with whether each reads here, and the
// one the person under ctx picked.
func (a *App) voicePersonas(ctx context.Context) (speech.VoicePersonas, error) {
	if a.Settings == nil {
		return speech.VoicePersonas{}, errors.New("settings aren't available")
	}
	out := speech.VoicePersonas{Personas: []speech.VoicePersona{}}
	for _, p := range a.storedPersonas(ctx) {
		v := speech.VoicePersona{Persona: p, Reads: a.voiceWorker != nil && a.voiceWorker.ValidVoice(p.Language, p.Voice)}
		if !v.Reads && a.voiceWorker != nil {
			for _, c := range a.voiceWorker.VoiceChoices(nil) {
				if c.Language == p.Language {
					v.ReadsWith = c.Automatic
				}
			}
		}
		out.Personas = append(out.Personas, v)
	}
	if p, ok := a.pickedPersona(ctx); ok {
		out.Picked = p.ID
	}
	return out, nil
}

// pickedPersona is the persona the person under ctx picked, if it's still
// there.
func (a *App) pickedPersona(ctx context.Context) (speech.Persona, bool) {
	id := a.personalString(ctx, settingVoicePersona, "")
	if id == "" {
		return speech.Persona{}, false
	}
	for _, p := range a.storedPersonas(ctx) {
		if p.ID == id {
			return p, true
		}
	}
	return speech.Persona{}, false
}

// pickVoicePersona has the person under ctx read aloud in persona id, ""
// for none.
func (a *App) pickVoicePersona(ctx context.Context, id string) error {
	if id != "" {
		found := false
		for _, p := range a.storedPersonas(ctx) {
			found = found || p.ID == id
		}
		if !found {
			return contracts.Errorf("PERSONA_NOT_FOUND", map[string]any{"id": id}, "there's no voice persona %q", id)
		}
	}
	return a.setPersonal(ctx, settingVoicePersona, id)
}

// checkPersona tidies p and says what's wrong with it.
func (a *App) checkPersona(p *speech.Persona) error {
	p.Name = strings.TrimSpace(p.Name)
	p.Language = strings.ToLower(strings.TrimSpace(p.Language))
	p.Voice = strings.TrimSpace(p.Voice)
	if p.Speed == 0 {
		p.Speed = 1
	}
	switch {
	case p.Name == "" || utf8.RuneCountInString(p.Name) > 40:
		return contracts.Errorf("PERSONA_NAME_INVALID", nil, "a persona's name is 1 to 40 characters")
	case !speech.ValidSpeed(p.Speed):
		return contracts.Errorf("PERSONA_SPEED_INVALID", nil, "a persona's speed is from %.1f to %.1f", speech.MinSpeed, speech.MaxSpeed)
	case a.voiceWorker == nil:
		return errors.New("speech isn't set up on this computer")
	case !a.voiceWorker.ValidVoice(p.Language, p.Voice):
		return contracts.Errorf("INVALID_VOICE", map[string]any{"language": p.Language, "voice": p.Voice}, "%q isn't a voice for %s here", p.Voice, p.Language)
	}
	return nil
}

// createVoicePersona adds a persona.
func (a *App) createVoicePersona(ctx context.Context, p speech.Persona) (speech.Persona, error) {
	if err := a.checkPersona(&p); err != nil {
		return p, err
	}
	list := a.storedPersonas(ctx)
	if len(list) >= maxPersonas {
		return p, contracts.Errorf("PERSONA_LIMIT", map[string]any{"limit": maxPersonas}, "there can be at most %d voice personas", maxPersonas)
	}
	p.ID = uuid.NewString()
	return p, a.savePersonas(ctx, append(list, p))
}

// updateVoicePersona changes persona id.
func (a *App) updateVoicePersona(ctx context.Context, id string, p speech.Persona) (speech.Persona, error) {
	if err := a.checkPersona(&p); err != nil {
		return p, err
	}
	list := a.storedPersonas(ctx)
	for i := range list {
		if list[i].ID == id {
			p.ID = id
			list[i] = p
			return p, a.savePersonas(ctx, list)
		}
	}
	return p, contracts.Errorf("PERSONA_NOT_FOUND", map[string]any{"id": id}, "there's no voice persona %q", id)
}

// deleteVoicePersona removes persona id. Whoever picked it goes back to
// the language's own voice, since a pick of a missing persona is none.
func (a *App) deleteVoicePersona(ctx context.Context, id string) error {
	list := a.storedPersonas(ctx)
	for i := range list {
		if list[i].ID == id {
			return a.savePersonas(ctx, append(list[:i], list[i+1:]...))
		}
	}
	return contracts.Errorf("PERSONA_NOT_FOUND", map[string]any{"id": id}, "there's no voice persona %q", id)
}

// personaStore is the personas, for the API.
type personaStore struct{ a *App }

func (s personaStore) List(ctx context.Context) (speech.VoicePersonas, error) {
	return s.a.voicePersonas(ctx)
}
func (s personaStore) Pick(ctx context.Context, id string) error {
	return s.a.pickVoicePersona(ctx, id)
}
func (s personaStore) Create(ctx context.Context, p speech.Persona) (speech.Persona, error) {
	return s.a.createVoicePersona(ctx, p)
}
func (s personaStore) Update(ctx context.Context, id string, p speech.Persona) (speech.Persona, error) {
	return s.a.updateVoicePersona(ctx, id, p)
}
func (s personaStore) Delete(ctx context.Context, id string) error {
	return s.a.deleteVoicePersona(ctx, id)
}
