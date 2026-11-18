package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/speech"
)

// Only an Admin sees and changes the Voice settings and installs Kitten
// (#1304).
func TestSpeechVoicesNeedAnAdmin(t *testing.T) {
	var set, installed, removed string
	srv := NewServer(Dependencies{
		SpeechVoices: func(context.Context) (speech.VoiceSettings, error) {
			return speech.VoiceSettings{Kitten: speech.KittenStatus{State: speech.KittenAbsent, Credit: speech.KittenCredit}}, nil
		},
		SetSpeechVoice: func(_ context.Context, lang, v string) error { set = lang + "=" + v; return nil },
		InstallKitten:  func(context.Context) error { installed = "yes"; return nil },
		RemoveKitten:   func(context.Context) error { removed = "yes"; return nil },
	})
	router := mux.NewRouter()
	srv.speechRoutes(router.PathPrefix("/api/v1").Subrouter())
	call := func(role auth.Role, method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r = r.WithContext(auth.WithPrincipal(r.Context(), auth.Principal{Person: auth.Person{ID: "p", Role: role}}))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/speech/voices", ""},
		{http.MethodPut, "/api/v1/speech/voices/de", `{"voice":"kitten:de:lena"}`},
		{http.MethodPost, "/api/v1/speech/kitten", ""},
		{http.MethodDelete, "/api/v1/speech/kitten", ""},
	} {
		if w := call(auth.RoleMember, c.method, c.path, c.body); w.Code != http.StatusForbidden {
			t.Fatalf("a Member's %s %s: %d", c.method, c.path, w.Code)
		}
		if w := call(auth.RoleAdmin, c.method, c.path, c.body); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Powered by KittenML") {
			t.Fatalf("an Admin's %s %s: %d %s", c.method, c.path, w.Code, w.Body)
		}
	}
	if set != "de=kitten:de:lena" || installed != "yes" || removed != "yes" {
		t.Fatalf("set %q installed %q removed %q", set, installed, removed)
	}
	if w := call(auth.RoleAdmin, http.MethodPut, "/api/v1/speech/voices/German", `{"voice":""}`); w.Code != http.StatusBadRequest {
		t.Fatalf("a language that isn't a code: %d", w.Code)
	}
}
