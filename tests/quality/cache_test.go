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
//   - New chats: a new chat with the same system prompt reads it from the
//     cache, even after a chat with another one. llama-server keeps recent
//     prompts in memory (--cache-ram), so a profile's long instructions are
//     read once, not per chat.
//   - Equivalence: an answer from a warm cache is the one a cold read gives.
//     At temperature 0 the warm and cold turn write the same tokens, with the
//     same top-5 probabilities within a tolerance.
//
// It runs against the served model (TOSKAR_QUALITY_MODEL_URL, as
// scripts/quality-model.sh starts it), and is skipped without one.
//
//	TOSKAR_QUALITY_CACHE_MIN_REUSE   least share of turn N's prompt from the cache (0.8)
//	TOSKAR_QUALITY_CACHE_TOLERANCE   most a top-5 probability may differ (0.05)
//	TOSKAR_QUALITY_CACHE_REPORT      writes the measurements as Markdown
//	TOSKAR_QUALITY_CACHE_EQUIVALENCE "measure" reports equivalence without failing on it

// cacheChat is a fixed conversation: a long system prompt and questions
// the model answers as it would in a chat, so each turn's prompt is the last
// one, its answer as the chat template writes it back, and one more question.
// At about 2,300 tokens it is longer than a sliding window (Gemma 3's is
// 1,024 tokens), where a model with one can lose what it read.
var cacheChat = struct {
	System    string
	Questions []string
}{
	System: "You are a careful assistant for a small hardware store in Juneau, Alaska. " + strings.Repeat(
		"Answer in plain words, name the aisle when you mention a product, and say when something is out of stock rather than guessing. ", 80),
	Questions: []string{
		"Do you carry rain gutters?",
		"What sealant should I use on the seams?",
		"Will it cure in 40 degree weather?",
		"How many hangers for 30 feet of gutter?",
		"Do you have downspout extensions?",
	},
}

// cacheAnswerTokens is how long each earlier answer may be.
const cacheAnswerTokens = 64

// otherSystem starts a chat with another assistant, between the store's
// chats, as a person switching profiles does.
var otherSystem = "You are a patient tutor for high school chemistry. " + strings.Repeat(
	"Explain each step, check units, and ask a question back when the student's question is unclear. ", 60)

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
// or a top-5 token's probability by more than tolerance; "" when they
// match. worst is the largest probability difference it saw. Probabilities,
// not log probabilities, are compared: the warm and cold turns read the
// prompt in different batches, which moves log probabilities by rounding,
// most where a token is unlikely (Gemma 3 gave one at -13.26 warm and
// -13.16 cold), without changing what the model would write.
//
// The tolerance comes from measuring (llama.cpp b11149, 2026-10-27): warm
// and cold wrote the same tokens every time, with probabilities at most
// 0.0075 apart for Llama 3.2 1B, Qwen 2.5 7B, and Gemma 2 2B, and up to
// 0.038 for Gemma 3 4B, as much inside its sliding window as beyond it and
// no less with --swa-full or without flash attention. A cold read alone is
// the same at any batch size. At a near-tie Gemma 3 can even write another
// word warm than cold, so its run measures equivalence without failing on
// it (TOSKAR_QUALITY_CACHE_EQUIVALENCE=measure) and is held to reuse.
func sameAnswer(warm, cold []servedLogprob, tolerance float64) (diff string, worst float64) {
	if len(warm) != len(cold) {
		return fmt.Sprintf("the warm answer has %d tokens and the cold one %d", len(warm), len(cold)), 0
	}
	for i := range warm {
		if warm[i].Token != cold[i].Token {
			return fmt.Sprintf("token %d is %q warm and %q cold", i, warm[i].Token, cold[i].Token), worst
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
			d := math.Abs(math.Exp(w.Logprob) - math.Exp(c))
			worst = max(worst, d)
			if d > tolerance {
				return fmt.Sprintf("at token %d, %q has probability %.4f warm and %.4f cold (%.4f apart)", i, w.Token, math.Exp(w.Logprob), math.Exp(c), d), worst
			}
		}
	}
	return "", worst
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
	for _, q := range cacheChat.Questions {
		messages = append(messages, map[string]string{"role": "user", "content": q})
		answer := askServed(t, base, messages, cacheAnswerTokens, true)
		turns = append(turns, answer.Timings)
		messages = append(messages, map[string]string{"role": "assistant", "content": answer.Text})
	}
	messages = append(messages, map[string]string{"role": "user", "content": cacheQuestion})

	warm := askServed(t, base, messages, 32, true)
	cold := askServed(t, base, messages, 32, false)

	// Another assistant's chat, then a new chat with the store's.
	askServed(t, base, []map[string]string{{"role": "system", "content": otherSystem}, {"role": "user", "content": "What is a mole?"}}, 1, true)
	newChat := askServed(t, base, []map[string]string{{"role": "system", "content": cacheChat.System}, {"role": "user", "content": "Where are the snow shovels?"}}, 1, true).Timings
	t.Logf("a new chat: %d of %d prompt tokens from the cache (%.0f%%)", newChat.CacheN, newChat.CacheN+newChat.PromptN, reuse(newChat)*100)
	if reuse(newChat) < minReuse {
		t.Errorf("only %.0f%% of a new chat's prompt came from the cache, after a chat with another system prompt; at least %.0f%% should", reuse(newChat)*100, minReuse*100)
	}

	got := reuse(warm.Timings)
	t.Logf("turn %d: %d of %d prompt tokens from the cache (%.0f%%)", len(cacheChat.Questions)+1, warm.Timings.CacheN, warm.Timings.CacheN+warm.Timings.PromptN, got*100)
	diff, worst := sameAnswer(warm.Logprobs, cold.Logprobs, tolerance)
	if path := config.Env("QUALITY_CACHE_REPORT"); path != "" {
		writeCacheReport(t, path, turns, warm, cold, newChat, got, minReuse, tolerance, worst, diff)
	}
	if got < minReuse {
		t.Errorf("only %.0f%% of turn %d's prompt came from the cache; at least %.0f%% should", got*100, len(cacheChat.Questions)+1, minReuse*100)
	}
	if cold.Timings.CacheN != 0 {
		t.Errorf("the cold turn read %d tokens from the cache; cache_prompt false should read none", cold.Timings.CacheN)
	}
	if len(warm.Logprobs) == 0 {
		t.Fatal("the model gave no log probabilities to compare")
	}
	if diff != "" {
		msg := fmt.Sprintf("the warm and cold answers differ: %s\nwarm: %q\ncold: %q", diff, warm.Text, cold.Text)
		if config.Env("QUALITY_CACHE_EQUIVALENCE") == "measure" {
			t.Log(msg)
		} else {
			t.Error(msg)
		}
	}
}

func writeCacheReport(t *testing.T, path string, turns []servedTiming, warm, cold servedTurn, newChat servedTiming, got, minReuse, tolerance, worst float64, diff string) {
	var b strings.Builder
	b.WriteString("# Prompt cache (#838)\n\n")
	fmt.Fprintf(&b, "Turn %d read **%.0f%%** of its prompt from the cache (%d of %d tokens); at least %.0f%% should.\n\n",
		len(turns)+1, got*100, warm.Timings.CacheN, warm.Timings.CacheN+warm.Timings.PromptN, minReuse*100)
	b.WriteString("| Turn | From the cache | Read |\n|---|---|---|\n")
	for i, tm := range turns {
		fmt.Fprintf(&b, "| %d | %d | %d |\n", i+1, tm.CacheN, tm.PromptN)
	}
	fmt.Fprintf(&b, "| %d (warm) | %d | %d |\n| %d (cold) | %d | %d |\n\n", len(turns)+1, warm.Timings.CacheN, warm.Timings.PromptN, len(turns)+1, cold.Timings.CacheN, cold.Timings.PromptN)
	fmt.Fprintf(&b, "A new chat with the same system prompt, after a chat with another, read **%.0f%%** from the cache (%d of %d tokens).\n\n", reuse(newChat)*100, newChat.CacheN, newChat.CacheN+newChat.PromptN)
	if diff == "" {
		fmt.Fprintf(&b, "The warm and cold answers match: the same %d tokens, top-5 probabilities at most %.4f apart (%.2f allowed).\n", len(warm.Logprobs), worst, tolerance)
	} else {
		fmt.Fprintf(&b, "**The warm and cold answers differ:** %s.\n", diff)
	}
	fmt.Fprintf(&b, "\n```text\nwarm: %s\ncold: %s\n```\n", warm.Text, cold.Text)
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Errorf("cache report: %v", err)
	}
}

func TestSameAnswer(t *testing.T) {
	tok := func(token string, top ...float64) servedLogprob {
		l := servedLogprob{Token: token, Logprob: top[0]}
		for i, lp := range top {
			l.Top = append(l.Top, struct {
				Token   string  `json:"token"`
				Logprob float64 `json:"logprob"`
			}{fmt.Sprint(token, i), lp})
		}
		return l
	}
	warm := []servedLogprob{tok("a", -0.05, -3.80, -13.26)}
	// Rounding: 0.1 apart in log probability, 0.002 in probability.
	if diff, worst := sameAnswer(warm, []servedLogprob{tok("a", -0.05, -3.70, -13.16)}, 0.01); diff != "" || worst > 0.003 {
		t.Errorf("rounding counted as a difference: %q (%v)", diff, worst)
	}
	// A cache that lost part of the chat moves likely tokens.
	if diff, _ := sameAnswer(warm, []servedLogprob{tok("a", -0.2, -3.80, -13.26)}, 0.05); diff == "" {
		t.Error("a 0.13 probability difference passed")
	}
	if diff, _ := sameAnswer(warm, []servedLogprob{tok("b", -0.05)}, 0.01); diff == "" {
		t.Error("a different token passed")
	}
	if diff, _ := sameAnswer(warm, nil, 0.01); diff == "" {
		t.Error("a shorter answer passed")
	}
}
