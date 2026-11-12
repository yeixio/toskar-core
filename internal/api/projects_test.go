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
	"github.com/yeixio/toskar-core/internal/mimir"
	"github.com/yeixio/toskar-core/internal/muninn"
	"github.com/yeixio/toskar-core/internal/projects"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/store/repositories"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// projectsServer is a server with projects and chats on a real database.
func projectsServer(t *testing.T) (*Server, *repositories.ConversationRepo) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	chats := repositories.NewConversationRepo(db.SQL)
	srv := NewServer(Dependencies{
		ListConversations: chats.List,
		CreateConversation: func(ctx context.Context, title, profileID, modelID string) (contracts.Conversation, error) {
			return chats.Create(ctx, title, profileID, modelID)
		},
		SetConversationProjectID: chats.SetProjectID,
		UpdateConversation: func(ctx context.Context, id string, title, profileID, modelID *string, memoryOff *bool) (contracts.Conversation, error) {
			return chats.Update(ctx, id, repositories.ConversationPatch{Title: title, ProfileID: profileID, ModelID: modelID, MemoryOff: memoryOff})
		},
	})
	srv.BindProjects(&projects.Service{Store: projects.NewStore(db.SQL),
		Files: mimir.NewStore(db.SQL, filepath.Join(t.TempDir(), "knowledge")), Memories: muninn.NewStore(db.SQL)})
	return srv, chats
}

// call makes a request as a person with a role, the way the auth middleware
// leaves it.
func call(h http.HandlerFunc, person string, role auth.Role, method, path string, vars map[string]string, body any) (int, string) {
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
	r = r.WithContext(auth.WithPrincipal(r.Context(), auth.Principal{Person: auth.Person{ID: person, Role: role}}))
	if vars != nil {
		r = mux.SetURLVars(r, vars)
	}
	rec := httptest.NewRecorder()
	h(rec, r)
	return rec.Code, rec.Body.String()
}

func TestProjectsAPI(t *testing.T) {
	srv, _ := projectsServer(t)
	h := func(f http.HandlerFunc) http.HandlerFunc { return srv.projectsReady(f) }
	code, body := call(h(srv.handleCreateProject), "vee", auth.RoleVisitor, http.MethodPost, "/api/v1/projects", nil,
		map[string]any{"name": "Garden", "instructions": "Answer in metric.", "memory_off": false})
	if code != http.StatusCreated {
		t.Fatalf("create: %d %s", code, body)
	}
	var p contracts.Project
	_ = json.Unmarshal([]byte(body), &p)
	id := map[string]string{"id": p.ID}
	if code, body := call(h(srv.handleCreateProject), "vee", auth.RoleVisitor, http.MethodPost, "/api/v1/projects", nil, map[string]any{"name": " "}); code != http.StatusBadRequest || !strings.Contains(body, "PROJECT_NAME_REQUIRED") {
		t.Fatalf("no name: %d %s", code, body)
	}

	// A folder is an Admin's to set, and has to be one.
	dir := t.TempDir()
	if code, body := call(h(srv.handleUpdateProject), "vee", auth.RoleMember, http.MethodPatch, "/", id, map[string]any{"folder": dir}); code != http.StatusForbidden || !strings.Contains(body, "ROLE_REQUIRED") {
		t.Fatalf("member's folder: %d %s", code, body)
	}
	if code, body := call(h(srv.handleUpdateProject), "vee", auth.RoleAdmin, http.MethodPatch, "/", id, map[string]any{"folder": filepath.Join(dir, "gone")}); code != http.StatusBadRequest || !strings.Contains(body, "NOT_A_FOLDER") {
		t.Fatalf("not a folder: %d %s", code, body)
	}
	if code, body := call(h(srv.handleUpdateProject), "vee", auth.RoleAdmin, http.MethodPatch, "/", id, map[string]any{"folder": dir}); code != http.StatusOK || !strings.Contains(body, `"folder":`) {
		t.Fatalf("admin's folder: %d %s", code, body)
	}
	// Knowledge is for Members and up to read, so to link.
	if code, body := call(h(srv.handleUpdateProject), "vee", auth.RoleVisitor, http.MethodPatch, "/", id, map[string]any{"knowledge": []string{"k1"}}); code != http.StatusForbidden {
		t.Fatalf("visitor's knowledge: %d %s", code, body)
	}
	if code, body := call(h(srv.handleUpdateProject), "vee", auth.RoleMember, http.MethodPatch, "/", id, map[string]any{"knowledge": []string{"k1"}}); code != http.StatusBadRequest || !strings.Contains(body, "KNOWLEDGE_SOURCE_NOT_FOUND") {
		t.Fatalf("unknown knowledge: %d %s", code, body)
	}

	// Files.
	file := map[string]string{"id": p.ID, "name": "beds.md"}
	if code, body := call(h(srv.handleAddProjectFile), "vee", auth.RoleVisitor, http.MethodPost, "/", id, map[string]any{"filename": "beds.md", "text": "# Beds\n\nTomatoes get morning sun."}); code != http.StatusCreated || !strings.Contains(body, `"files":[{"name":"beds.md"`) {
		t.Fatalf("add file: %d %s", code, body)
	}
	if code, body := call(h(srv.handleAddProjectFile), "vee", auth.RoleVisitor, http.MethodPost, "/", id, map[string]any{"filename": "run.exe", "content_base64": "TVo="}); code != http.StatusBadRequest {
		t.Fatalf("unsupported file: %d %s", code, body)
	}
	if code, body := call(h(srv.handleListProjects), "vee", auth.RoleVisitor, http.MethodGet, "/", nil, nil); code != http.StatusOK || !strings.Contains(body, `"name":"Garden"`) || !strings.Contains(body, `"chat_count":0`) {
		t.Fatalf("list: %d %s", code, body)
	}

	// Another person sees none of it.
	if code, body := call(h(srv.handleListProjects), "sam", auth.RoleAdmin, http.MethodGet, "/", nil, nil); code != http.StatusOK || body != "[]\n" {
		t.Fatalf("sam's list: %d %s", code, body)
	}
	for name, try := range map[string]func() (int, string){
		"get": func() (int, string) {
			return call(h(srv.handleGetProject), "sam", auth.RoleAdmin, http.MethodGet, "/", id, nil)
		},
		"patch": func() (int, string) {
			return call(h(srv.handleUpdateProject), "sam", auth.RoleAdmin, http.MethodPatch, "/", id, map[string]any{"name": "Mine"})
		},
		"add file": func() (int, string) {
			return call(h(srv.handleAddProjectFile), "sam", auth.RoleAdmin, http.MethodPost, "/", id, map[string]any{"filename": "x.md", "text": "x"})
		},
		"remove file": func() (int, string) {
			return call(h(srv.handleRemoveProjectFile), "sam", auth.RoleAdmin, http.MethodDelete, "/", file, nil)
		},
		"delete": func() (int, string) {
			return call(h(srv.handleDeleteProject), "sam", auth.RoleAdmin, http.MethodDelete, "/", id, nil)
		},
	} {
		if code, body := try(); code != http.StatusNotFound || !strings.Contains(body, "PROJECT_NOT_FOUND") {
			t.Errorf("sam's %s: %d %s", name, code, body)
		}
	}

	if code, body := call(h(srv.handleRemoveProjectFile), "vee", auth.RoleVisitor, http.MethodDelete, "/", file, nil); code != http.StatusNoContent {
		t.Fatalf("remove file: %d %s", code, body)
	}
	if code, body := call(h(srv.handleRemoveProjectFile), "vee", auth.RoleVisitor, http.MethodDelete, "/", file, nil); code != http.StatusNotFound || !strings.Contains(body, "PROJECT_FILE_NOT_FOUND") {
		t.Fatalf("remove again: %d %s", code, body)
	}
	if code, body := call(h(srv.handleDeleteProject), "vee", auth.RoleVisitor, http.MethodDelete, "/api/v1/projects/x?chats=all", id, nil); code != http.StatusBadRequest {
		t.Fatalf("chats=all: %d %s", code, body)
	}
	if code, body := call(h(srv.handleDeleteProject), "vee", auth.RoleVisitor, http.MethodDelete, "/", id, nil); code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", code, body)
	}
	if code, _ := call(h(srv.handleGetProject), "vee", auth.RoleVisitor, http.MethodGet, "/", id, nil); code != http.StatusNotFound {
		t.Fatalf("deleted project: %d", code)
	}
}

// A chat goes in one of the person's own projects, when it's made or
// later, and a project's chats are listed by its id (#1136).
func TestConversationsInProjects(t *testing.T) {
	srv, chats := projectsServer(t)
	vee := auth.WithPrincipal(context.Background(), auth.Principal{Person: auth.Person{ID: "vee", Role: auth.RoleVisitor}})
	sam := auth.WithPrincipal(context.Background(), auth.Principal{Person: auth.Person{ID: "sam", Role: auth.RoleMember}})
	name := "Garden"
	mine, err := srv.projects.Create(vee, projects.Fields{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := srv.projects.Create(sam, projects.Fields{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	if code, body := call(srv.handleCreateConversation, "vee", auth.RoleVisitor, http.MethodPost, "/", nil, map[string]any{"title": "x", "project_id": theirs.ID}); code != http.StatusNotFound || !strings.Contains(body, "PROJECT_NOT_FOUND") {
		t.Fatalf("in sam's project: %d %s", code, body)
	}
	code, body := call(srv.handleCreateConversation, "vee", auth.RoleVisitor, http.MethodPost, "/", nil, map[string]any{"title": "beds", "project_id": mine.ID})
	if code != http.StatusCreated || !strings.Contains(body, `"project_id":"`+mine.ID+`"`) {
		t.Fatalf("create in project: %d %s", code, body)
	}
	other, err := chats.Create(vee, "elsewhere", "", "")
	if err != nil {
		t.Fatal(err)
	}
	list := func() []contracts.Conversation {
		code, body := call(srv.handleListConversations, "vee", auth.RoleVisitor, http.MethodGet, "/api/v1/conversations?project_id="+mine.ID, nil, nil)
		var out []contracts.Conversation
		if code != http.StatusOK || json.Unmarshal([]byte(body), &out) != nil {
			t.Fatalf("list: %d %s", code, body)
		}
		return out
	}
	if got := list(); len(got) != 1 || got[0].Title != "beds" {
		t.Fatalf("the project's chats: %+v", got)
	}
	vars := map[string]string{"id": other.ID}
	if code, body := call(srv.handleUpdateConversation, "vee", auth.RoleVisitor, http.MethodPatch, "/", vars, map[string]any{"project_id": theirs.ID}); code != http.StatusNotFound {
		t.Fatalf("move into sam's: %d %s", code, body)
	}
	if code, body := call(srv.handleUpdateConversation, "vee", auth.RoleVisitor, http.MethodPatch, "/", vars, map[string]any{"project_id": mine.ID}); code != http.StatusOK || !strings.Contains(body, mine.ID) {
		t.Fatalf("move in: %d %s", code, body)
	}
	if got := list(); len(got) != 2 {
		t.Fatalf("after moving in: %+v", got)
	}
	if code, body := call(srv.handleUpdateConversation, "vee", auth.RoleVisitor, http.MethodPatch, "/", vars, map[string]any{"project_id": ""}); code != http.StatusOK || strings.Contains(body, "project_id") {
		t.Fatalf("move out: %d %s", code, body)
	}
	if got := list(); len(got) != 1 {
		t.Fatalf("after moving out: %+v", got)
	}
	// Sam can't put Vee's chat in their project, or see it there.
	if code, _ := call(srv.handleUpdateConversation, "sam", auth.RoleMember, http.MethodPatch, "/", vars, map[string]any{"project_id": theirs.ID}); code == http.StatusOK {
		t.Fatal("sam moved vee's chat")
	}
	if p, _ := srv.projects.Get(sam, theirs.ID); p.ChatCount != 0 {
		t.Fatalf("sam's project has %d chats", p.ChatCount)
	}
}

// POST /chat takes one of the person's own projects for a new chat.
func TestChatInProject(t *testing.T) {
	srv, _ := projectsServer(t)
	var asked []string
	srv.deps.Chat = func(_ http.ResponseWriter, r *http.Request, _, _, _, _ string, _ bool, _ string) error {
		asked = append(asked, projects.IDFrom(r.Context()))
		return nil
	}
	name := "Garden"
	sam := auth.WithPrincipal(context.Background(), auth.Principal{Person: auth.Person{ID: "sam", Role: auth.RoleMember}})
	theirs, err := srv.projects.Create(sam, projects.Fields{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	if code, body := call(srv.handleChat, "vee", auth.RoleVisitor, http.MethodPost, "/", nil, map[string]any{"message": "hi", "project_id": theirs.ID}); code != http.StatusNotFound || !strings.Contains(body, "PROJECT_NOT_FOUND") {
		t.Fatalf("in sam's project: %d %s", code, body)
	}
	if code, body := call(srv.handleChat, "sam", auth.RoleMember, http.MethodPost, "/", nil, map[string]any{"message": "hi", "project_id": theirs.ID}); code != http.StatusOK {
		t.Fatalf("in their own: %d %s", code, body)
	}
	if len(asked) != 1 || asked[0] != theirs.ID {
		t.Fatalf("asked %v", asked)
	}
}

// Visitors and up use their own projects; a phone's key lists them and
// starts chats in them; a portal's guests have none (#1136).
func TestProjectRoutesRoles(t *testing.T) {
	for _, route := range []string{"GET /api/v1/projects", "POST /api/v1/projects", "GET /api/v1/projects/{id}", "PATCH /api/v1/projects/{id}",
		"DELETE /api/v1/projects/{id}", "POST /api/v1/projects/{id}/files", "DELETE /api/v1/projects/{id}/files/{name}"} {
		method, path, _ := strings.Cut(route, " ")
		if got := auth.RoleNeeded(method, path); got != auth.RoleVisitor {
			t.Errorf("%s needs %s", route, got)
		}
		if guestRoutes[route] {
			t.Errorf("a portal's guest reaches %s", route)
		}
		if read := method == http.MethodGet; auth.DeviceMayReach(method, path) != read {
			t.Errorf("a phone reaches %s: %v", route, !read)
		}
	}
	for _, route := range []string{"POST /api/v1/conversations", "POST /api/v1/chat", "GET /api/v1/conversations"} {
		method, path, _ := strings.Cut(route, " ")
		if !auth.DeviceMayReach(method, path) {
			t.Errorf("a phone can't reach %s", route)
		}
	}
}

// The knowledge routes treat a project's files as not found, whoever asks
// (#1136): they're reached through the project.
func TestKnowledgeRoutesHideProjectFiles(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	kb := mimir.NewStore(db.SQL, filepath.Join(t.TempDir(), "knowledge"))
	srv := NewServer(Dependencies{})
	srv.BindKnowledge(kb)
	if err := kb.AddProjectFile(context.Background(), "garden", "beds.md", []byte("# Beds\n\nThe tomato bed gets morning sun.")); err != nil {
		t.Fatal(err)
	}
	source, _ := kb.ProjectSourceID(context.Background(), "garden")
	vars := map[string]string{"id": source}
	for name, try := range map[string]func() (int, string){
		"get": func() (int, string) {
			return call(srv.handleGetKnowledge, "o", auth.RoleOwner, http.MethodGet, "/", vars, nil)
		},
		"update": func() (int, string) {
			return call(srv.handleUpdateKnowledge, "o", auth.RoleOwner, http.MethodPatch, "/", vars, map[string]any{"name": "x"})
		},
		"content": func() (int, string) {
			return call(srv.handleKnowledgeContent, "o", auth.RoleOwner, http.MethodGet, "/", vars, nil)
		},
		"refresh": func() (int, string) {
			return call(srv.handleRefreshKnowledge, "o", auth.RoleOwner, http.MethodPost, "/", vars, nil)
		},
		"delete": func() (int, string) {
			return call(srv.handleDeleteKnowledge, "o", auth.RoleOwner, http.MethodDelete, "/", vars, nil)
		},
	} {
		if code, body := try(); code != http.StatusNotFound {
			t.Errorf("%s: %d %s", name, code, body)
		}
	}
	code, body := call(srv.handleSearchKnowledge, "o", auth.RoleOwner, http.MethodPost, "/", nil, map[string]any{"query": "tomato bed morning sun", "source_ids": []string{source}})
	if code != http.StatusOK || strings.Contains(body, "morning sun") {
		t.Fatalf("search: %d %s", code, body)
	}
	if code, body := call(srv.handleListKnowledge, "o", auth.RoleOwner, http.MethodGet, "/", nil, nil); code != http.StatusOK || body != "[]\n" {
		t.Fatalf("list: %d %s", code, body)
	}
}

// The Memory page moves a project's memory to the person's own with
// PATCH /memory/{id} and project_id "" (#1136).
func TestPatchMemoryToPersonal(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	memories := muninn.NewStore(db.SQL)
	srv := NewServer(Dependencies{})
	srv.BindMemory(memories)
	ctx := auth.WithPrincipal(context.Background(), auth.Principal{Person: auth.Person{ID: "vee", Role: auth.RoleMember}})
	m, _, err := memories.AddIn(ctx, "garden", "The garden soil is clay", "", muninn.SourceExplicit, "")
	if err != nil {
		t.Fatal(err)
	}
	id := map[string]string{"id": m.ID}
	if code, body := call(srv.handleListMemory, "vee", auth.RoleMember, http.MethodGet, "/", nil, nil); code != http.StatusOK || !strings.Contains(body, `"project_id":"garden"`) {
		t.Fatalf("list: %d %s", code, body)
	}
	if code, body := call(srv.handlePatchMemory, "vee", auth.RoleMember, http.MethodPatch, "/", id, map[string]any{"project_id": "taxes"}); code != http.StatusBadRequest {
		t.Fatalf("into a project: %d %s", code, body)
	}
	if code, body := call(srv.handlePatchMemory, "sam", auth.RoleMember, http.MethodPatch, "/", id, map[string]any{"project_id": ""}); code != http.StatusNotFound {
		t.Fatalf("another person's: %d %s", code, body)
	}
	code, body := call(srv.handlePatchMemory, "vee", auth.RoleMember, http.MethodPatch, "/", id, map[string]any{"project_id": ""})
	if code != http.StatusOK || strings.Contains(body, "project_id") {
		t.Fatalf("moved: %d %s", code, body)
	}
}
