package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Only an Admin can give a chat a folder, since that lets it run things on this
// computer, and the folder has to be there (#1017).
func TestConversationProject(t *testing.T) {
	dir := t.TempDir()
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	var set []string
	srv := NewServer(Dependencies{
		SetConversationProject: func(_ context.Context, _, project string) error {
			set = append(set, project)
			return nil
		},
		UpdateConversation: func(_ context.Context, id string, _, _, _ *string, _ *bool) (contracts.Conversation, error) {
			return contracts.Conversation{ID: id}, nil
		},
	})
	patch := func(role auth.Role, project string) (int, string) {
		raw, _ := json.Marshal(map[string]string{"project": project})
		r := httptest.NewRequest(http.MethodPatch, "/api/v1/conversations/c1", strings.NewReader(string(raw)))
		r = r.WithContext(auth.WithPrincipal(r.Context(), auth.Principal{Person: auth.Person{ID: "p", Role: role}}))
		r = mux.SetURLVars(r, map[string]string{"id": "c1"})
		rec := httptest.NewRecorder()
		srv.handleUpdateConversation(rec, r)
		return rec.Code, rec.Body.String()
	}
	if code, body := patch(auth.RoleMember, dir); code != http.StatusForbidden || !strings.Contains(body, "ROLE_REQUIRED") {
		t.Fatalf("member: %d %s", code, body)
	}
	if code, body := patch(auth.RoleOwner, dir); code != http.StatusOK {
		t.Fatalf("set: %d %s", code, body)
	}
	if code, body := patch(auth.RoleOwner, filepath.Join(dir, "gone")); code != http.StatusBadRequest || !strings.Contains(body, "NOT_A_FOLDER") {
		t.Fatalf("gone: %d %s", code, body)
	}
	if code, body := patch(auth.RoleOwner, "relative/path"); code != http.StatusBadRequest {
		t.Fatalf("relative: %d %s", code, body)
	}
	if code, body := patch(auth.RoleOwner, ""); code != http.StatusOK {
		t.Fatalf("clear: %d %s", code, body)
	}
	if len(set) != 2 || set[0] != real || set[1] != "" {
		t.Fatalf("set %v", set)
	}
}
