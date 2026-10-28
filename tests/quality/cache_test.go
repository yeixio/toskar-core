package quality

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/config"
)

// The prompt cache (#838). llama-server keeps the last prompt's work, so a
// chat's next turn reads only what's new. Two things must hold for the
// models Toskar runs:
//
//   - Reuse: on a chat's later turns, most of the prompt comes from the
//     cache. Some models, such as those with sliding-window attention, can
//     defeat it, and a long chat then re-reads everything each turn.
//   - Equivalence: an answer from a warm cache is the one a cold read gives.
//     At temperature 0 the warm and cold turn write the same tokens, with the
//     same top-5 log probabilities within a tolerance.
//
// It runs against the served model (TOSKAR_QUALITY_MODEL_URL, as
// scripts/quality-model.sh starts it), and is skipped without one.
//
//	TOSKAR_QUALITY_CACHE_MIN_REUSE   least share of turn N's prompt from the cache (0.8)
//	TOSKAR_QUALITY_CACHE_TOLERANCE   most a top-5 log probability may differ (0.05)
//	TOSKAR_QUALITY_CACHE_REPORT      writes the measurements as Markdown

// cacheChat is a fixed conversation: a long system prompt and turns whose
// answers are written out, so every turn's prompt starts with the last one's.
var cacheChat = struct {
	System string
	Turns  [][2]string
}{
	System: "You are a careful assistant for a small hardware store in Juneau, Alaska. " + strings.Repeat(
		"Answer in plain words, name the aisle when you mention a product, and say when something is out of stock rather than guessing. ", 12),
	Turns: [][2]string{
		{"Do you carry rain gutters?", "Yes. Vinyl gutters are in aisle 7, in 10-foot lengths, with end caps and hangers beside them."},
		{"What sealant should I use on the seams?", "Use the gutter sealant in aisle 7, on the shelf under the gutters. It stays flexible in the cold."},
		{"Will it cure in 40 degree weather?", "It needs 40°F or warmer for a day. Below that it cures slowly, so pick a dry afternoon."},
		{"How many hangers for 30 feet of gutter?", "One hanger every 2 feet, so 15 hangers for 30 feet, plus one at each end: 17."},
		{"Do you have downspout extensions?", "Yes, aisle 7 again: flexible extensions that stretch from 2 to 6 feet."},
	},
}

// cacheQuestion is turn N, asked warm and cold.
const cacheQuestion = "Summarize what I need to buy for 30 feet of gutter, with the aisle for each item."

type servedTiming struct {
	CacheN  int `json:"cache_n"`
	PromptN int `json:"prompt_n"`
}

type servedLogprob struct {
	Token   string  `json:"token"`
	Logprob float64 `json:"logprob"`
	Top     []struct {
		Token   string  `json:"token"`
		Logprob float64 `json:"logprob"`
	} `json:"top_logprobs"`
}

type servedTurn struct {
	Text     string
	Timings  servedTiming
	Logprobs []servedLogprob
}

// askServed sends one chat to an OpenAI-compatible llama-server at
// temperature 0, with or without its prompt cache.
func askServed(t *testing.T, base string, messages []map[string]string, maxTokens int, cache bool) servedTurn {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"messages": messages, "temperature": 0, "seed": 1, "max_tokens": maxTokens,
		"logprobs": true, "top_logprobs": 5, "cache_prompt": cache,
	})
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Post(base+"/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("model: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("model: %s: %.300s", resp.Status, raw)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Logprobs struct {
				Content []servedLogprob `json:"content"`
			} `json:"logprobs"`
		} `json:"choices"`
		Timings servedTiming `json:"timings"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 {
		t.Fatalf("model: unreadable reply %.300s", raw)
	}
	c := out.Choices[0]
	return servedTurn{Text: c.Message.Content, Timings: out.Timings, Logprobs: c.Logprobs.Content}
}

// reuse is the share of a prompt read from the cache.
func reuse(t servedTiming) float64 {
	if t.CacheN+t.PromptN == 0 {
		return 0
	}
	return float64(t.CacheN) / float64(t.CacheN+t.PromptN)
}

// sameAnswer reports where a warm and a cold answer first differ: a token,
// or a top-5 log probability by more than tolerance; "" when they match.
func sameAnswer(warm, cold []servedLogprob, tolerance float64) string {
	if len(warm) != len(cold) {
		return fmt.Sprintf("the warm answer has %d tokens and the cold one %d", len(warm), len(cold))
	}
	for i := range warm {
		if warm[i].Token != cold[i].Token {
			return fmt.Sprintf("token %d is %q warm and %q cold", i, warm[i].Token, cold[i].Token)
		}
		coldTop := map[string]float64{}
		for _, c := range cold[i].Top {
			coldTop[c.Token] = c.Logprob
		}
		for _, w := range warm[i].Top {
			c, ok := coldTop[w.Token]
			if !ok {
				continue // a near-tie at the edge of the top 5
			}
			if d := math.Abs(w.Logprob - c); d > tolerance {
				return fmt.Sprintf("at token %d, %q has log probability %.4f warm and %.4f cold (%.4f apart)", i, w.Token, w.Logprob, c, d)
			}
		}
	}
	return ""
}

func envFloat(name string, fallback float64) float64 {
	if v, err := strconv.ParseFloat(config.Env(name), 64); err == nil && v > 0 {
		return v
	}
	return fallback
}

func TestPromptCache(t *testing.T) {
	base := strings.TrimRight(config.Env("QUALITY_MODEL_URL"), "/")
	if base == "" {
		t.Skip("needs a served model: TOSKAR_QUALITY_MODEL_URL")
	}
	minReuse := envFloat("QUALITY_CACHE_MIN_REUSE", 0.8)
	tolerance := envFloat("QUALITY_CACHE_TOLERANCE", 0.05)

	// The chat, one turn at a time, as a person sends it: each prompt is
	// the last one and one more turn, so the cache warms as it goes.
	messages := []map[string]string{{"role": "system", "content": cacheChat.System}}
	var turns []servedTiming
	for _, turn := range cacheChat.Turns {
		messages = append(messages, map[string]string{"role": "user", "content": turn[0]})
		turns = append(turns, askServed(t, base, messages, 1, true).Timings)
		messages = append(messages, map[string]string{"role": "assistant", "content": turn[1]})
	}
	messages = append(messages, map[string]string{"role": "user", "content": cacheQuestion})

	warm := askServed(t, base, messages, 32, true)
	cold := askServed(t, base, messages, 32, false)

	got := reuse(warm.Timings)
	t.Logf("turn %d: %d of %d prompt tokens from the cache (%.0f%%)", len(cacheChat.Turns)+1, warm.Timings.CacheN, warm.Timings.CacheN+warm.Timings.PromptN, got*100)
	diff := sameAnswer(warm.Logprobs, cold.Logprobs, tolerance)
	if path := config.Env("QUALITY_CACHE_REPORT"); path != "" {
		writeCacheReport(t, path, turns, warm, cold, got, minReuse, tolerance, diff)
	}
	if got < minReuse {
		t.Errorf("only %.0f%% of turn %d's prompt came from the cache; at least %.0f%% should", got*100, len(cacheChat.Turns)+1, minReuse*100)
	}
	if cold.Timings.CacheN != 0 {
		t.Errorf("the cold turn read %d tokens from the cache; cache_prompt false should read none", cold.Timings.CacheN)
	}
	if len(warm.Logprobs) == 0 {
		t.Fatal("the model gave no log probabilities to compare")
	}
	if diff != "" {
		t.Errorf("the warm and cold answers differ: %s\nwarm: %q\ncold: %q", diff, warm.Text, cold.Text)
	}
}

func writeCacheReport(t *testing.T, path string, turns []servedTiming, warm, cold servedTurn, got, minReuse, tolerance float64, diff string) {
	var b strings.Builder
	b.WriteString("# Prompt cache (#838)\n\n")
	fmt.Fprintf(&b, "Turn %d read **%.0f%%** of its prompt from the cache (%d of %d tokens); at least %.0f%% should.\n\n",
		len(turns)+1, got*100, warm.Timings.CacheN, warm.Timings.CacheN+warm.Timings.PromptN, minReuse*100)
	b.WriteString("| Turn | From the cache | Read |\n|---|---|---|\n")
	for i, tm := range turns {
		fmt.Fprintf(&b, "| %d | %d | %d |\n", i+1, tm.CacheN, tm.PromptN)
	}
	fmt.Fprintf(&b, "| %d (warm) | %d | %d |\n| %d (cold) | %d | %d |\n\n", len(turns)+1, warm.Timings.CacheN, warm.Timings.PromptN, len(turns)+1, cold.Timings.CacheN, cold.Timings.PromptN)
	if diff == "" {
		fmt.Fprintf(&b, "The warm and cold answers match: the same %d tokens, top-5 log probabilities within %.2f.\n", len(warm.Logprobs), tolerance)
	} else {
		fmt.Fprintf(&b, "**The warm and cold answers differ:** %s.\n", diff)
	}
	fmt.Fprintf(&b, "\n```text\nwarm: %s\ncold: %s\n```\n", warm.Text, cold.Text)
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Errorf("cache report: %v", err)
	}
}
