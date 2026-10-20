package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/automations"
)

// Only an Admin or the Owner gives an automation cameras, a live view of
// the home; a Member's automation without them is fine (#667).
func TestAutomationCamerasNeedAnAdmin(t *testing.T) {
	created := 0
	srv := NewServer(Dependencies{
		CreateAutomation: func(_ context.Context, in automations.CreateInput) (automations.Automation, error) {
			created++
			return automations.Automation{ID: "a", Cameras: in.Cameras}, nil
		},
		UpdateAutomation: func(_ context.Context, id string, _ automations.Patch) (automations.Automation, error) {
			return automations.Automation{ID: id}, nil
		},
	})
	call := func(role auth.Role, method, path, body string) int {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r = r.WithContext(auth.WithPrincipal(r.Context(), auth.Principal{Person: auth.Person{ID: "p", Role: role}}))
		w := httptest.NewRecorder()
		if method == http.MethodPost {
			srv.handleCreateAutomation(w, r)
		} else {
			srv.handleUpdateAutomation(w, r)
		}
		return w.Code
	}
	cams := `{"name":"n","prompt":"p","cameras":[{"connector":"homeassistant","device":"camera.porch"}]}`
	if code := call(auth.RoleMember, http.MethodPost, "/api/v1/automations", cams); code != http.StatusForbidden {
		t.Fatalf("a Member's cameras: %d", code)
	}
	if code := call(auth.RoleMember, http.MethodPatch, "/api/v1/automations/a", `{"cameras":[{"connector":"homeassistant","device":"camera.porch"}]}`); code != http.StatusForbidden {
		t.Fatalf("a Member adding cameras: %d", code)
	}
	if code := call(auth.RoleMember, http.MethodPatch, "/api/v1/automations/a", `{"cameras":[]}`); code != http.StatusOK {
		t.Fatalf("a Member removing cameras: %d", code)
	}
	if code := call(auth.RoleMember, http.MethodPost, "/api/v1/automations", `{"name":"n","prompt":"p"}`); code != http.StatusCreated {
		t.Fatalf("a Member's automation without cameras: %d", code)
	}
	for _, role := range []auth.Role{auth.RoleAdmin, auth.RoleOwner} {
		if code := call(role, http.MethodPost, "/api/v1/automations", cams); code != http.StatusCreated {
			t.Fatalf("%s's cameras: %d", role, code)
		}
	}
	if created != 3 {
		t.Fatalf("created %d", created)
	}
}
