package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Speaking a message (yeixio/toskar-apps#155): a recording's text, or why
// it can't be had.
func TestTranscribe(t *testing.T) {
	var gotID, gotLang string
	srv := NewServer(Dependencies{
		Transcribe: func(_ context.Context, id, lang string) (map[string]any, error) {
			if id == "none" {
				return nil, contracts.NewError("SPEECH_UNAVAILABLE", map[string]any{"detail": "not set up"}, errors.New("not set up"))
			}
			gotID, gotLang = id, lang
			return map[string]any{"text": "what plant is this", "language": "en", "duration_seconds": 2.1}, nil
		},
	})
	call := func(body string) (int, map[string]any) {
		w := httptest.NewRecorder()
		srv.handleTranscribe(w, httptest.NewRequest(http.MethodPost, "/api/v1/speech/transcribe", strings.NewReader(body)))
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	status, out := call(`{"artifact_id":"a1","language":"en"}`)
	if status != 200 || out["text"] != "what plant is this" || gotID != "a1" || gotLang != "en" {
		t.Fatalf("transcribe: %d %v (%s %s)", status, out, gotID, gotLang)
	}
	if status, out := call(`{"artifact_id":"none"}`); status != http.StatusServiceUnavailable || out["error"].(map[string]any)["code"] != "SPEECH_UNAVAILABLE" {
		t.Fatalf("unavailable: %d %v", status, out)
	}
	if status, _ := call(`{}`); status != http.StatusBadRequest {
		t.Fatalf("no recording: %d", status)
	}
}
