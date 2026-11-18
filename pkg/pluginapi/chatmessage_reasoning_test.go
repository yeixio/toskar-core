package pluginapi

import (
	"encoding/json"
	"strings"
	"testing"
)

// An assistant step's thinking is sent as reasoning_content and read back,
// and a message without any sends no field (#841).
func TestChatMessageReasoningContent(t *testing.T) {
	b, _ := json.Marshal(ChatMessage{Role: "assistant", Content: "x", Reasoning: "plan"})
	if string(b) != `{"role":"assistant","content":"x","reasoning_content":"plan"}` {
		t.Fatalf("got %s", b)
	}
	b, _ = json.Marshal(ChatMessage{Role: "user", Content: "x"})
	if strings.Contains(string(b), "reasoning") {
		t.Fatalf("got %s", b)
	}
	b, _ = json.Marshal(ChatMessage{Role: "assistant", Content: "x", Images: []string{"data:image/png;base64,AA=="}, Reasoning: "plan"})
	if !strings.Contains(string(b), `"reasoning_content":"plan"`) {
		t.Fatalf("with images: %s", b)
	}
	var m ChatMessage
	if err := json.Unmarshal([]byte(`{"role":"assistant","content":"x","reasoning_content":"plan"}`), &m); err != nil || m.Reasoning != "plan" {
		t.Fatalf("read %+v, %v", m, err)
	}
}
