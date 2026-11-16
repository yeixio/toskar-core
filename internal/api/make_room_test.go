package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/turnopts"
)

// Only an Admin may stop the models kept running for an answer, as only an
// Admin chooses what's kept running (#966).
func TestMakeRoomNeedsAnAdmin(t *testing.T) {
	var makeRoom, called bool
	srv := NewServer(Dependencies{
		Chat: func(w http.ResponseWriter, r *http.Request, _, _, _, _ string, _ bool, _ string) error {
			called, makeRoom = true, turnopts.MakeRoomFrom(r.Context())
			w.WriteHeader(http.StatusOK)
			return nil
		},
	})
	call := func(role auth.Role, body string) int {
		called, makeRoom = false, false
		r := httptest.NewRequest(http.MethodPost, "/api/v1/chat", strings.NewReader(body))
		r = r.WithContext(auth.WithPrincipal(r.Context(), auth.Principal{Person: auth.Person{ID: "p", Role: role}}))
		w := httptest.NewRecorder()
		srv.handleChat(w, r)
		return w.Code
	}
	if code := call(auth.RoleMember, `{"message":"hi","make_room":true}`); code != http.StatusForbidden || called {
		t.Fatalf("a Member making room: %d, chat called %v", code, called)
	}
	if code := call(auth.RoleAdmin, `{"message":"hi","make_room":true}`); code != http.StatusOK || !makeRoom {
		t.Fatalf("an Admin making room: %d, make room %v", code, makeRoom)
	}
	if code := call(auth.RoleMember, `{"message":"hi"}`); code != http.StatusOK || makeRoom {
		t.Fatalf("a plain chat: %d, make room %v", code, makeRoom)
	}
}
