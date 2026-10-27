package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// POST /chat with thinking "off" asks a reasoning model to answer without
// thinking first (#840), as reasoning_effort "none" does on the API.
func TestChatThinkingOff(t *testing.T) {
	mgr, err := config.NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var got pluginapi.GenerateOptions
	ran := false
	srv := NewServer(Dependencies{
		Config: mgr,
		Chat: func(w http.ResponseWriter, r *http.Request, _, _, _, _ string, _ bool, _ string) error {
			got, ran = pluginapi.AnswerOptionsFrom(r.Context()), true
			w.WriteHeader(http.StatusOK)
			return nil
		},
	})
	call := func(body string) int {
		ran = false
		r := httptest.NewRequest(http.MethodPost, "/api/v1/chat", strings.NewReader(body))
		r.RemoteAddr, r.Host = "127.0.0.1:50000", "127.0.0.1:7331"
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, r)
		return rec.Code
	}
	for body, want := range map[string]bool{
		`{"message":"hi","thinking":"off"}`: true,
		`{"message":"hi","thinking":"on"}`:  false,
		`{"message":"hi"}`:                  false,
	} {
		if code := call(body); code != http.StatusOK || !ran || got.NoThinking != want {
			t.Errorf("%s: %d, no thinking %v", body, code, got.NoThinking)
		}
	}
	if code := call(`{"message":"hi","thinking":"maybe"}`); code != http.StatusBadRequest || ran {
		t.Errorf("an unknown value: %d, ran %v", code, ran)
	}
}
