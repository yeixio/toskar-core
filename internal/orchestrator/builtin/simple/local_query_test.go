package simple

import (
	"context"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// A search for places near the person names their town (#1381).
func TestLocalQuery(t *testing.T) {
	for _, c := range []struct{ query, prompt, location, want string }{
		{"restaurants in town", "What restaurants are in town?", "Juneau, AK", "restaurants in Juneau, AK"},
		{"good sushi near me", "good sushi near me?", "Juneau, AK", "good sushi in Juneau, AK"},
		{"Where should we go for dinner", "Where should we go for dinner?", "Juneau, AK", "Where should we go for dinner in Juneau, AK"},
		{"cafes downtown Sitka", "cafes downtown Sitka", "Juneau, AK", "cafes downtown Sitka"},
		{"restaurants in town", "What restaurants are in town?", "", "restaurants in town"},
	} {
		if got := localQuery(c.query, c.prompt, c.location); got != c.want {
			t.Errorf("localQuery(%q) = %q, want %q", c.query, got, c.want)
		}
	}
}

// placesEnv answers a web search with one restaurant, and knows where the
// person is.
type placesEnv struct {
	scriptedEnv
	location string
	queries  []string
}

func (e *placesEnv) ExecuteTool(ctx context.Context, toolID string, args map[string]any) (map[string]any, error) {
	e.tools++
	if toolID == "internet.search" {
		q, _ := args["query"].(string)
		e.queries = append(e.queries, q)
		return map[string]any{"results": []any{map[string]any{"title": "The Real Diner", "url": "https://example.com/diner", "snippet": "Diner on Main St"}}}, nil
	}
	return map[string]any{}, nil
}

func (e *placesEnv) UserLocation(ctx context.Context) string { return e.location }

func runPlaces(t *testing.T, env pluginapi.ExecutionEnvironment, policy, prompt string) {
	t.Helper()
	events, err := New().Run(context.Background(), contracts.Task{Prompt: prompt}, contracts.AIProfile{
		Roles: []contracts.ModelRole{{Role: "assistant", ModelID: "m"}},
		Tools: []contracts.ToolPolicy{{ToolID: "internet.search", Policy: policy}},
	}, env)
	if err != nil {
		t.Fatal(err)
	}
	for range events {
	}
}

// "What restaurants are in town?" is looked up in the person's town, and
// the answer may name only what was found (#1381).
func TestRestaurantsInTownAreLookedUp(t *testing.T) {
	env := &placesEnv{location: "Juneau, AK", scriptedEnv: scriptedEnv{replies: []string{"The Real Diner."}}}
	runPlaces(t, env, "allow", "What restaurants are in town?")
	if len(env.queries) != 1 || !strings.Contains(env.queries[0], "in Juneau, AK") || strings.Contains(env.queries[0], "town") {
		t.Fatalf("queries = %q", env.queries)
	}
	sys := env.seen[len(env.seen)-1][0].Content
	if !strings.Contains(sys, groundingGuidance) || !strings.Contains(sys, "never ones from memory") {
		t.Fatalf("system prompt lacks grounding: %s", sys)
	}
}

// With web search set to ask, nothing is looked up first, and the model is
// told not to list places from memory (#1381).
func TestRestaurantsWithoutLookupAreNotMadeUp(t *testing.T) {
	env := &placesEnv{location: "Juneau, AK", scriptedEnv: scriptedEnv{replies: []string{"I couldn't look that up."}}}
	runPlaces(t, env, "ask", "What restaurants are in town?")
	if len(env.queries) != 0 {
		t.Fatalf("searched without asking: %q", env.queries)
	}
	sys := env.seen[0][0].Content
	if !strings.Contains(sys, placesGuidance) {
		t.Fatalf("system prompt lacks places guidance: %s", sys)
	}
}
