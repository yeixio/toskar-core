package speech

import (
	"context"
	"strings"

	"github.com/yeixio/toskar-core/internal/replylang"
)

// Persona is a voice an Admin made for people to pick (#1319): a name, the
// language it's for, one of that language's voices, and how fast it reads.
// It reads its own language in its voice; another language is read in that
// language's voice, at the persona's speed.
type Persona struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Language string  `json:"language"`
	Voice    string  `json:"voice"`
	Speed    float64 `json:"speed"`
}

// VoicePersona is a persona as the Voice settings show it: Reads is false
// when its voice can't read here now, such as a Kitten voice after Kitten
// was removed, and ReadsWith is then the voice that reads in its place.
type VoicePersona struct {
	Persona
	Reads     bool   `json:"reads"`
	ReadsWith string `json:"reads_with,omitempty"`
}

// VoicePersonas are the personas and the one the person picked.
type VoicePersonas struct {
	Personas []VoicePersona `json:"personas"`
	Picked   string         `json:"picked"`
}

// The speeds a persona reads at: half to twice as fast.
const (
	MinSpeed = 0.5
	MaxSpeed = 2.0
)

// ValidSpeed says whether s is a speed a persona or a request may read at.
func ValidSpeed(s float64) bool { return s >= MinSpeed && s <= MaxSpeed }

type speedKey struct{}

// WithSpeed has what's read aloud under ctx read at speed, where 1 is
// normal; a speed out of range is ignored.
func WithSpeed(ctx context.Context, speed float64) context.Context {
	if !ValidSpeed(speed) {
		return ctx
	}
	return context.WithValue(ctx, speedKey{}, speed)
}

// speedOf is ctx's reading speed, 0 when none was set.
func speedOf(ctx context.Context) float64 {
	s, _ := ctx.Value(speedKey{}).(float64)
	return s
}

// withSpeed adds ctx's speed to a worker request when it isn't normal.
func withSpeed(ctx context.Context, req map[string]any) map[string]any {
	if s := speedOf(ctx); s != 0 && s != 1 {
		req["speed"] = s
	}
	return req
}

// persona applies the persona the person under ctx picked, when voice
// wasn't asked for: its voice for its own language, and its speed.
func (w *Worker) persona(ctx context.Context, text, voice, language string) (context.Context, string) {
	if voice != "" || w.Persona == nil || speedOf(ctx) != 0 {
		return ctx, voice
	}
	p, ok := w.Persona(ctx)
	if !ok {
		return ctx, voice
	}
	ctx = WithSpeed(ctx, p.Speed)
	lang := language
	if lang == "" {
		lang, _ = replylang.Detect(text)
	}
	base, _, _ := strings.Cut(strings.ToLower(lang), "-")
	if p.Voice != "" && (base == "" || base == p.Language) && w.ValidVoice(p.Language, p.Voice) {
		return ctx, p.Voice
	}
	return ctx, voice
}

// Personas are the Voice settings' personas (#1319): everyone lists them
// and picks one for themselves; Admins make, change, and delete them.
type Personas interface {
	List(ctx context.Context) (VoicePersonas, error)
	Pick(ctx context.Context, id string) error
	Create(ctx context.Context, p Persona) (Persona, error)
	Update(ctx context.Context, id string, p Persona) (Persona, error)
	Delete(ctx context.Context, id string) error
}
