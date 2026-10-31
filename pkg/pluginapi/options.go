package pluginapi

import "context"

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
