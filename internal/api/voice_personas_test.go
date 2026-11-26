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
	"github.com/yeixio/toskar-core/pkg/contracts"
)

type fakePersonas struct{ picked, made, changed, deleted string }

func (f *fakePersonas) List(context.Context) (speech.VoicePersonas, error) {
	return speech.VoicePersonas{Personas: []speech.VoicePersona{{Persona: speech.Persona{ID: "g", Name: "Greta"}, Reads: true}}, Picked: f.picked}, nil
}
func (f *fakePersonas) Pick(_ context.Context, id string) error {
	if id == "nobody" {
		return contracts.Errorf("PERSONA_NOT_FOUND", nil, "no")
	}
	f.picked = id
	return nil
}
func (f *fakePersonas) Create(_ context.Context, p speech.Persona) (speech.Persona, error) {
	f.made = p.Name
	return p, nil
}
func (f *fakePersonas) Update(_ context.Context, id string, p speech.Persona) (speech.Persona, error) {
	f.changed = id + "=" + p.Name
	return p, nil
}
func (f *fakePersonas) Delete(_ context.Context, id string) error { f.deleted = id; return nil }

// Everyone lists personas and picks their own; only an Admin makes,
// changes, or deletes one (#1319).
func TestVoicePersonaRoutes(t *testing.T) {
	f := &fakePersonas{}
	srv := NewServer(Dependencies{VoicePersonas: f})
	router := mux.NewRouter()
	srv.voicePersonaRoutes(router.PathPrefix("/api/v1").Subrouter())
	call := func(role auth.Role, method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r = r.WithContext(auth.WithPrincipal(r.Context(), auth.Principal{Person: auth.Person{ID: "p", Role: role}}))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	if w := call(auth.RoleMember, http.MethodGet, "/api/v1/voice/personas", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Greta") {
		t.Fatalf("a Member's list: %d %s", w.Code, w.Body)
	}
	if w := call(auth.RoleMember, http.MethodPut, "/api/v1/voice/persona", `{"id":"g"}`); w.Code != http.StatusOK || f.picked != "g" {
		t.Fatalf("a Member's pick: %d %s", w.Code, w.Body)
	}
	if w := call(auth.RoleMember, http.MethodPut, "/api/v1/voice/persona", `{"id":"nobody"}`); w.Code != http.StatusNotFound {
		t.Fatalf("a missing persona: %d", w.Code)
	}
	for _, c := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/voice/personas", `{"name":"Max"}`},
		{http.MethodPut, "/api/v1/voice/personas/g", `{"name":"Gretel"}`},
		{http.MethodDelete, "/api/v1/voice/personas/g", ""},
	} {
		if w := call(auth.RoleMember, c.method, c.path, c.body); w.Code != http.StatusForbidden {
			t.Fatalf("a Member's %s %s: %d", c.method, c.path, w.Code)
		}
		if w := call(auth.RoleAdmin, c.method, c.path, c.body); w.Code != http.StatusOK {
			t.Fatalf("an Admin's %s %s: %d %s", c.method, c.path, w.Code, w.Body)
		}
	}
	if f.made != "Max" || f.changed != "g=Gretel" || f.deleted != "g" {
		t.Fatalf("%+v", f)
	}
}
