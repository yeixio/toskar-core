package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// completion posts a chat completion with one picture, as an OpenAI client
// sends it, from this computer.
func completion(t *testing.T, a *App, text string) (int, map[string]any) {
	t.Helper()
	picture := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\n pixels"))
	body, _ := json.Marshal(map[string]any{
		"model": "auto",
		"messages": []any{map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": text},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": picture}},
		}}},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(body)))
	req.Host, req.RemoteAddr = "127.0.0.1:7331", "127.0.0.1:50000"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	a.API.Handler().ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// A picture sent to /v1/chat/completions reaches a model that sees (#830),
// as one attached in the web chat does, and isn't kept afterwards.
func TestCompletionPicturesReachAModelThatSees(t *testing.T) {
	a, used, sent := pictureApp(t, true)
	code, out := completion(t, a, "What is in this picture?")
	if code != http.StatusOK {
		t.Fatalf("%d %v", code, out)
	}
	if len(*used) == 0 || (*used)[len(*used)-1] != "see-model" {
		t.Fatalf("models = %v", *used)
	}
	if msg := lastUser((*sent)[len(*sent)-1]); len(msg.Images) != 1 || !strings.HasPrefix(msg.Images[0], "data:image/png;base64,") {
		t.Fatalf("the picture didn't reach the model: %+v", msg)
	} else if !strings.Contains(msg.Content, "It is shown to you") {
		t.Fatalf("the model wasn't told the picture is shown: %q", msg.Content)
	}
	if left, _ := a.Artifacts.List(context.Background(), ""); len(left) != 0 {
		t.Fatalf("the request's pictures were kept: %+v", left)
	}
}

// With no model that sees, the answer says so, rather than answering as if
// the picture had been seen.
func TestCompletionPicturesWithoutAModelThatSees(t *testing.T) {
	a, used, sent := pictureApp(t, false)
	noVisionToInstall(a)
	code, out := completion(t, a, "Tell me about this photo.")
	if code != http.StatusOK {
		t.Fatalf("%d %v", code, out)
	}
	if len(lastUser((*sent)[len(*sent)-1]).Images) != 0 || (*used)[len(*used)-1] != "text-model" {
		t.Fatalf("models = %v", *used)
	}
	if !strings.Contains(lastUser((*sent)[len(*sent)-1]).Content, "You cannot see it") {
		t.Fatalf("the model wasn't told it can't see the picture: %q", lastUser((*sent)[len(*sent)-1]).Content)
	}
	raw, _ := json.Marshal(out)
	if !strings.Contains(string(raw), "No installed model can see pictures") {
		t.Fatalf("the answer doesn't say no model can see: %s", raw)
	}
}
