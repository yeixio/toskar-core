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
// chat's next turn reads only what's new. These must hold for the
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
//     At temperature 0 the warm and cold turn write the same tokens, each
//     with the same probability within a tolerance, until they part at a
//     near-tie, if they do.
//   - Recall: the warm turn still knows what the chat began with, the
//     store's city, whenever the cold read does. A cache that lost the start
//     of a chat, as a sliding window can, answers another city.
//
// It runs against the served model (TOSKAR_QUALITY_MODEL_URL, as
// scripts/quality-model.sh starts it), and is skipped without one.
//
//	TOSKAR_QUALITY_CACHE_MIN_REUSE   least share of turn N's prompt from the cache (0.8)
//	TOSKAR_QUALITY_CACHE_TOLERANCE   most a written token's probability may differ (0.2)
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

// cacheQuestion is turn N, asked warm and cold. Its answer depends on the
// chat, and its first words aren't a near-tie: "Summarize what I need to
// buy" opened with "To buy" or "To summarize", "To" or "Based", which on a
// CPU the batch size alone decides (see sameAnswer), leaving nothing to
// compare.
const cacheQuestion = "Write a short shopping list for this job."

// recallQuestion asks, warm and cold, for what the chat's first sentence
// says. Llama 3.2 1B answers "Juneau" warm, cold, and cold in other
// batches, and "Chicago" from a chat without its first sentence, without
// the first half of its system prompt, or without one at all.
const recallQuestion, recallAnswer = "What city is your store in? Answer with the city only.", "juneau"

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

// sameAnswer reports how a warm and a cold answer differ: "" when they
// match, or part only at a near-tie. They match while they write the same
// tokens, each with the same probability within tolerance. Where they write
// different tokens, it must be a near-tie: each read has both tokens in its
// top 5, and neither token's probability moved by more than tolerance.
// What follows is another answer, so parted is where they part (-1 if they
// don't) and nothing after it is compared. written is the largest
// probability difference of a token both answers wrote, others of a top-5
// token they didn't. Probabilities, not log probabilities, are compared: a
// log probability moves most where a token is unlikely (Gemma 3 gave one at
// -13.26 warm and -13.16 cold) without changing what the model writes.
//
// The warm and cold turns read the prompt in different batches (the cache's
// last tokens one at a time as they were written, a cold read 512 at a
// time), and on a CPU that alone moves probabilities. Measured with
// llama.cpp b11174 on a CPU (2026-10-31), Llama 3.2 1B's cold read in
// batches of 32 instead of 512, no cache involved, moved a written token's
// probability by up to 0.12 and an unwritten one's by up to 0.15, most at a
// near-tie, and there it can change the word: the batch size alone made
// Llama answer an earlier cacheQuestion with "To buy" or "To summarize". On
// the CI runner the same chat's answers parted at " job" or " gutter",
// either way round, after written tokens at most 0.037 apart; Gemma 3 4B's
// were up to 0.25 apart with the same words. Earlier measurements on a Mac's
// GPU (b11149) found at most 0.0075 for Llama 3.2 1B, Qwen 2.5 7B, and
// Gemma 2 2B, and 0.038 for Gemma 3 4B.
//
// So a cache that lost a little of the chat can't be told from batching
// here: simulated losses (a turn, half the system prompt, half the last
// answer) moved probabilities by no more than batching does, at the same
// near-ties. What this catches is a cache that moves what the model is sure
// of; recallQuestion catches one that lost the start of the chat. Gemma 3
// can write another word warm than cold even away from a near-tie, so its
// run measures equivalence without failing on it
// (TOSKAR_QUALITY_CACHE_EQUIVALENCE=measure), and is held to reuse and
// recall.
func sameAnswer(warm, cold []servedLogprob, tolerance float64) (diff string, parted int, written, others float64) {
	top := func(l servedLogprob) map[string]float64 {
		m := map[string]float64{}
		for _, t := range l.Top {
			m[t.Token] = math.Exp(t.Logprob)
		}
		return m
	}
	for i := range min(len(warm), len(cold)) {
		warmTop, coldTop := top(warm[i]), top(cold[i])
		if w, c := warm[i].Token, cold[i].Token; w != c {
			wCold, ok1 := coldTop[w]
			cWarm, ok2 := warmTop[c]
			if !ok1 || !ok2 {
				return fmt.Sprintf("token %d is %q warm and %q cold, which isn't a near-tie: each isn't in the other's top 5", i, w, c), i, written, others
			}
			wWarm, cCold := math.Exp(warm[i].Logprob), math.Exp(cold[i].Logprob)
			if d := max(math.Abs(wWarm-wCold), math.Abs(cWarm-cCold)); d > tolerance {
				return fmt.Sprintf("token %d is %q warm and %q cold, which isn't a near-tie: %q has probability %.4f warm and %.4f cold, %q %.4f and %.4f", i, w, c, w, wWarm, wCold, c, cWarm, cCold), i, written, others
			}
			return "", i, written, others
		}
		d := math.Abs(math.Exp(warm[i].Logprob) - math.Exp(cold[i].Logprob))
		written = max(written, d)
		if d > tolerance {
			return fmt.Sprintf("at token %d, %q has probability %.4f warm and %.4f cold (%.4f apart)", i, warm[i].Token, math.Exp(warm[i].Logprob), math.Exp(cold[i].Logprob), d), -1, written, others
		}
		for t, p := range warmTop {
			if c, ok := coldTop[t]; ok && t != warm[i].Token {
				others = max(others, math.Abs(p-c))
			}
		}
	}
	if len(warm) != len(cold) {
		return fmt.Sprintf("the warm answer has %d tokens and the cold one %d", len(warm), len(cold)), -1, written, others
	}
	return "", -1, written, others
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
	tolerance := envFloat("QUALITY_CACHE_TOLERANCE", 0.2)

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

	recall := append(messages[:len(messages)-1:len(messages)-1], map[string]string{"role": "user", "content": recallQuestion})
	warm := askServed(t, base, messages, 32, true)
	warmRecall := askServed(t, base, recall, 8, true)
	cold := askServed(t, base, messages, 32, false)
	coldRecall := askServed(t, base, recall, 8, false)

	// Another assistant's chat, then a new chat with the store's.
	askServed(t, base, []map[string]string{{"role": "system", "content": otherSystem}, {"role": "user", "content": "What is a mole?"}}, 1, true)
	newChat := askServed(t, base, []map[string]string{{"role": "system", "content": cacheChat.System}, {"role": "user", "content": "Where are the snow shovels?"}}, 1, true).Timings
	t.Logf("a new chat: %d of %d prompt tokens from the cache (%.0f%%)", newChat.CacheN, newChat.CacheN+newChat.PromptN, reuse(newChat)*100)
	if reuse(newChat) < minReuse {
		t.Errorf("only %.0f%% of a new chat's prompt came from the cache, after a chat with another system prompt; at least %.0f%% should", reuse(newChat)*100, minReuse*100)
	}

	got := reuse(warm.Timings)
	t.Logf("turn %d: %d of %d prompt tokens from the cache (%.0f%%)", len(cacheChat.Questions)+1, warm.Timings.CacheN, warm.Timings.CacheN+warm.Timings.PromptN, got*100)
	diff, parted, written, others := sameAnswer(warm.Logprobs, cold.Logprobs, tolerance)
	t.Logf("warm and cold: written tokens' probabilities at most %.4f apart (%.2f allowed), other top-5 tokens' %.4f", written, tolerance, others)
	if diff == "" && parted >= 0 {
		t.Logf("warm and cold part at token %d, a near-tie: %q warm and %q cold", parted, warm.Logprobs[parted].Token, cold.Logprobs[parted].Token)
	}
	t.Logf("recall: %q warm (%d of %d prompt tokens from the cache), %q cold", warmRecall.Text, warmRecall.Timings.CacheN, warmRecall.Timings.CacheN+warmRecall.Timings.PromptN, coldRecall.Text)
	if strings.Contains(strings.ToLower(coldRecall.Text), recallAnswer) && !strings.Contains(strings.ToLower(warmRecall.Text), recallAnswer) {
		t.Errorf("the warm turn lost the start of the chat: asked %q, it answered %q warm and %q cold", recallQuestion, warmRecall.Text, coldRecall.Text)
	}
	if path := config.Env("QUALITY_CACHE_REPORT"); path != "" {
		writeCacheReport(t, path, turns, warm, cold, newChat, got, minReuse, tolerance, written, others, diff, parted, warmRecall.Text, coldRecall.Text)
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

func writeCacheReport(t *testing.T, path string, turns []servedTiming, warm, cold servedTurn, newChat servedTiming, got, minReuse, tolerance, written, others float64, diff string, parted int, warmRecall, coldRecall string) {
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
	switch {
	case diff != "":
		fmt.Fprintf(&b, "**The warm and cold answers differ:** %s.\n", diff)
	case parted >= 0:
		fmt.Fprintf(&b, "The warm and cold answers match for %d tokens, their probabilities at most %.4f apart (%.2f allowed), then part at a near-tie, %q warm and %q cold. Other top-5 tokens' were at most %.4f apart, which batching alone can move.\n", parted, written, tolerance, warm.Logprobs[parted].Token, cold.Logprobs[parted].Token, others)
	default:
		fmt.Fprintf(&b, "The warm and cold answers match: the same %d tokens, their probabilities at most %.4f apart (%.2f allowed). Other top-5 tokens' were at most %.4f apart, which batching alone can move.\n", len(warm.Logprobs), written, tolerance, others)
	}
	fmt.Fprintf(&b, "\n```text\nwarm: %s\ncold: %s\n```\n", warm.Text, cold.Text)
	fmt.Fprintf(&b, "\nAsked %q, the warm turn answered %q and the cold one %q.\n", recallQuestion, warmRecall, coldRecall)
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Errorf("cache report: %v", err)
	}
}

func TestSameAnswer(t *testing.T) {
	// tok is a token written with probability p, and the other top-5 tokens
	// as name, probability pairs.
	tok := func(token string, p float64, others ...any) servedLogprob {
		l := servedLogprob{Token: token, Logprob: math.Log(p)}
		top := append([]any{token, p}, others...)
		for i := 0; i < len(top); i += 2 {
			l.Top = append(l.Top, struct {
				Token   string  `json:"token"`
				Logprob float64 `json:"logprob"`
			}{top[i].(string), math.Log(top[i+1].(float64))})
		}
		return l
	}
	answer := func(l ...servedLogprob) []servedLogprob { return l }

	// Rounding: an unlikely token 0.1 apart in log probability, nothing in
	// probability.
	if diff, parted, written, others := sameAnswer(answer(tok("a", 0.95, "b", 0.022, "c", 1.75e-6)), answer(tok("a", 0.95, "b", 0.0247, "c", 1.93e-6)), 0.2); diff != "" || parted != -1 || written != 0 || others > 0.003 {
		t.Errorf("rounding counted as a difference: %q (%d, %v, %v)", diff, parted, written, others)
	}
	// Batching (run 41228247242): the same token written, an unwritten one
	// 0.1012 warm and 0.1826 cold.
	diff, parted, written, others := sameAnswer(answer(tok(" buy", 0.30, " summarize", 0.1012)), answer(tok(" buy", 0.26, " summarize", 0.1826)), 0.2)
	if diff != "" || parted != -1 {
		t.Errorf("batching on an unwritten token counted as a difference: %q", diff)
	}
	if math.Abs(written-0.04) > 1e-9 || math.Abs(others-0.0814) > 1e-9 {
		t.Errorf("measured %v written and %v others; want 0.04 and 0.0814", written, others)
	}
	// Batching at a near-tie: the answers part, and what follows isn't
	// compared.
	warm := answer(tok("To", 0.47, "Based", 0.40), tok(" buy", 0.29, " summarize", 0.12), tok(" 30", 0.99))
	cold := answer(tok("To", 0.41, "Based", 0.48), tok(" summarize", 0.26, " buy", 0.21), tok(",", 0.99))
	if diff, parted, _, _ := sameAnswer(warm, cold, 0.2); diff != "" || parted != 1 {
		t.Errorf("a near-tie: %q, parted at %d; want parted at 1", diff, parted)
	}
	// A cache that moves what the model is sure of.
	if diff, _, _, _ := sameAnswer(answer(tok("a", 0.95, "b", 0.03)), answer(tok("a", 0.6, "b", 0.37)), 0.2); diff == "" {
		t.Error("a 0.35 probability difference on a written token passed")
	}
	if diff, _, _, _ := sameAnswer(answer(tok("Do", 0.93, "You", 0.003)), answer(tok("You", 0.77, "Do", 0.23)), 0.2); diff == "" {
		t.Error("a sure token turned over passed as a near-tie")
	}
	if diff, _, _, _ := sameAnswer(answer(tok("a", 0.95)), answer(tok("b", 0.9)), 0.2); diff == "" {
		t.Error("a token outside the other's top 5 passed")
	}
	if diff, _, _, _ := sameAnswer(answer(tok("a", 0.95)), nil, 0.2); diff == "" {
		t.Error("a shorter answer passed")
	}
}
