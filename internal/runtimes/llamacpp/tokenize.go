package llamacpp

import (
	"context"
	"fmt"
)

// Tokenize counts the tokens text takes with the model a llama-server is
// running, without the special tokens a chat template adds (AI experience
// spec §66).
func Tokenize(ctx context.Context, endpoint, text string) (int, error) {
	if endpoint == "" {
		return 0, fmt.Errorf("model endpoint required")
	}
	var out struct {
		Tokens *[]any `json:"tokens"`
	}
	if err := postJSON(ctx, endpoint+"/tokenize", map[string]any{"content": text, "add_special": false}, &out); err != nil {
		return 0, err
	}
	if out.Tokens == nil {
		return 0, fmt.Errorf("llama-server returned no tokens")
	}
	return len(*out.Tokens), nil
}
