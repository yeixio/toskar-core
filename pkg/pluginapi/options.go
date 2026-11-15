package pluginapi

import (
	"context"
	"errors"
)

// GenerateOptions limit one model call: a sampling temperature and a cap on
// the reply's tokens (#459, docs/deliberate.md). Zero leaves the model's
// defaults; a Temperature below zero asks for temperature 0, the most
// likely reply every time (GreedyTemperature). They ride on the call's
// context, so an orchestrator sets them for one Generate without changing
// ExecutionEnvironment, and the call carries them to whichever computer
// runs it.
type GenerateOptions struct {
	Temperature float64
	MaxTokens   int
	// NoThinking asks a reasoning model to answer without thinking first
	// (#840), as reasoning_effort "none" does on the OpenAI-compatible API.
	NoThinking bool
	// Thinking asks a model that answers first by default to think before
	// it answers (#593), as thinking "on" or a reasoning_effort other than
	// none does.
	Thinking bool
	// ReasoningEffort is how long a model that sets its own thinking's
	// length should think: low, medium, or high, as gpt-oss's chat template
	// takes it (#1140). Empty leaves the model's own, medium for gpt-oss.
	ReasoningEffort string
	// Sampling is the caller's sampler settings and seed (#839).
	Sampling Sampling
}

// Sampling is what a caller sets of how the next token is picked (#839).
// Zero leaves the model's own setting, and a nil Seed a random one. The
// same seed and settings repeat an answer on one model, alone; under load
// llama.cpp batches requests together, which can change it (docs/api.md).
type Sampling struct {
	TopP float64 `json:"top_p,omitempty"`
	TopK int     `json:"top_k,omitempty"`
	MinP float64 `json:"min_p,omitempty"`
	Seed *int64  `json:"seed,omitempty"`
}

// Check says what's out of range, naming the field.
func (s Sampling) Check() error {
	switch {
	case s.TopP < 0 || s.TopP > 1:
		return errors.New("top_p must be between 0 and 1")
	case s.TopK < 0:
		return errors.New("top_k must be 0 or more")
	case s.MinP < 0 || s.MinP > 1:
		return errors.New("min_p must be between 0 and 1")
	case s.Seed != nil && *s.Seed < 0:
		return errors.New("seed must be 0 or more")
	}
	return nil
}

// Apply adds the settings that are set to an OpenAI-style request body,
// as llama-server takes them.
func (s Sampling) Apply(body map[string]any) {
	if s.TopP > 0 {
		body["top_p"] = s.TopP
	}
	if s.TopK > 0 {
		body["top_k"] = s.TopK
	}
	if s.MinP > 0 {
		body["min_p"] = s.MinP
	}
	if s.Seed != nil {
		body["seed"] = *s.Seed
	}
}

// GreedyTemperature is GenerateOptions.Temperature for temperature 0,
// which the zero value can't say. A runtime sends it as 0.
const GreedyTemperature = -1

// SamplingTemperature is the temperature to send a runtime for t, and
// whether to send one: 0 leaves the runtime's default.
func SamplingTemperature(t float64) (float64, bool) {
	switch {
	case t < 0:
		return 0, true
	case t > 0:
		return t, true
	}
	return 0, false
}

type generateOptionsKey struct{}

// WithGenerateOptions sets the options for calls made with ctx.
func WithGenerateOptions(ctx context.Context, o GenerateOptions) context.Context {
	return context.WithValue(ctx, generateOptionsKey{}, o)
}

// GenerateOptionsFrom is the options set on ctx, or none.
func GenerateOptionsFrom(ctx context.Context) GenerateOptions {
	o, _ := ctx.Value(generateOptionsKey{}).(GenerateOptions)
	return o
}

type answerOptionsKey struct{}

// WithAnswerOptions sets the options for the calls that write a turn's
// answer, such as an API caller's temperature and max_tokens (#70). Its
// planning, checks, and summaries keep their own.
func WithAnswerOptions(ctx context.Context, o GenerateOptions) context.Context {
	return context.WithValue(ctx, answerOptionsKey{}, o)
}

// AnswerOptionsFrom is the answer's options set on ctx, or none.
func AnswerOptionsFrom(ctx context.Context) GenerateOptions {
	o, _ := ctx.Value(answerOptionsKey{}).(GenerateOptions)
	return o
}
