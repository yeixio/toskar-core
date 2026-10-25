package openai

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// One answer's chunks, as a turn sends them: thinking, the answer, then
// done with what the runtime measured, cut off at the token limit.
func goldenChunks() []pluginapi.ChatChunk {
	return []pluginapi.ChatChunk{
		{Reasoning: "The user wants a greeting."},
		{Content: "Hello, "},
		{Content: "Juneau"},
		{Done: true, Metrics: &pluginapi.GenerationMetrics{PromptTokens: 12, CompletionTokens: 7, TotalTokens: 19, CachedTokens: 8, ReasoningTokens: 5, FinishReason: "length"}},
	}
}

var (
	goldenID      = regexp.MustCompile(`"id":"chatcmpl-[0-9a-f]{24}"`)
	goldenCreated = regexp.MustCompile(`"created":[0-9]{10}`)
)

// golden compares a response with testdata/name, with its id and time
// made stable. UPDATE_OPENAI_GOLDEN=1 writes the file.
func golden(t *testing.T, name, got string) {
	t.Helper()
	if !goldenID.MatchString(got) || !goldenCreated.MatchString(got) {
		t.Fatalf("%s: no id of its own or no created: %s", name, got)
	}
	got = goldenCreated.ReplaceAllString(goldenID.ReplaceAllString(got, `"id":"chatcmpl-ID"`), `"created":1700000000`)
	if strings.HasSuffix(name, ".json") {
		got = indented(t, strings.TrimSpace(got))
	}
	path := filepath.Join("testdata", name)
	if os.Getenv("UPDATE_OPENAI_GOLDEN") != "" {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (UPDATE_OPENAI_GOLDEN=1 writes it)", err)
	}
	if got != string(want) {
		t.Fatalf("%s differs from testdata (UPDATE_OPENAI_GOLDEN=1 rewrites it):\n got: %s\nwant: %s", name, got, want)
	}
}

func indented(t *testing.T, raw string) string {
	t.Helper()
	var b bytes.Buffer
	if err := json.Indent(&b, []byte(raw), "", "  "); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	return b.String() + "\n"
}

// The whole and the streamed answer, built from the same chunks (#832):
// an id of their own, created, the real finish_reason (length here, cut
// off), and usage with cached and reasoning tokens. A failed stream ends
// with an error and [DONE].
func TestCompletionGolden(t *testing.T) {
	h := testHandler(t)
	ask := `{"model":"profile:general","include_reasoning":true,"messages":[{"role":"user","content":"Say hello"}]`

	h.Chat = &scriptChat{chunks: goldenChunks()}
	rec := post(h, ask+`}`)
	golden(t, "completion.json", rec.Body.String())

	h.Chat = &scriptChat{chunks: goldenChunks()}
	rec = post(h, ask+`,"stream":true,"stream_options":{"include_usage":true}}`)
	golden(t, "completion-stream.txt", rec.Body.String())
	ids := goldenID.FindAllString(rec.Body.String(), -1)
	for _, id := range ids {
		if id != ids[0] {
			t.Fatalf("a stream's chunks have different ids: %v", ids)
		}
	}

	h.Chat = &scriptChat{chunks: []pluginapi.ChatChunk{{Content: "Hel"}, {Error: "the model stopped answering", Done: true}}}
	rec = post(h, ask+`,"stream":true}`)
	body := rec.Body.String()
	if !strings.Contains(body, `"type":"server_error"`) || !strings.Contains(body, `"code":"CHAT_FAILED"`) || !strings.HasSuffix(body, "data: [DONE]\n\n") {
		t.Fatalf("a failed stream: %s", body)
	}
}

// Errors are OpenAI's shape: message, type, param, and code.
func TestErrorShape(t *testing.T) {
	h := testHandler(t)
	rec := post(h, `{"model":"profile:general","messages":[{"role":"user","content":"x"}],"response_format":{"type":"xml"}}`)
	var out struct {
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Error["type"] != "invalid_request_error" || out.Error["param"] != "response_format" || out.Error["code"] != "INVALID_RESPONSE_FORMAT" || out.Error["message"] == "" {
		t.Fatalf("%s", rec.Body)
	}
	rec = post(h, `{"model":"profile:general","messages":[]}`)
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if _, has := out.Error["param"]; !has {
		t.Fatalf("param should be null, not missing: %s", rec.Body)
	}
}
