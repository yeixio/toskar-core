package api

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/store/repositories"
	"github.com/yeixio/toskar-core/internal/swarm"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// testSwarms is the swarm service over a real store: Start makes the record
// and a coordinator chat with two agent chats, as a planned swarm has, and
// Stop records which chats it stopped, each only the caller's.
type testSwarms struct {
	st      *swarm.Store
	chats   *repositories.ConversationRepo
	tools   *tools.Registry
	mu      sync.Mutex
	running map[string]string // chat → person
	stopped []string
}

func (s *testSwarms) Start(ctx context.Context, in swarm.Start) (contracts.Swarm, error) {
	coord, err := s.chats.Create(ctx, swarm.JobName(in.Prompt), "", "")
	if err != nil {
		return contracts.Swarm{}, err
	}
	rec, err := s.st.Create(ctx, in.ProjectID, coord.ID, swarm.ModeFor(in.Mode, nil, in.Budget), in.Prompt, swarm.BudgetFor(nil, in.Budget, 0, false).Contract())
	if err != nil {
		return contracts.Swarm{}, err
	}
	_ = s.chats.SetSwarm(ctx, coord.ID, rec.ID, "")
	s.mu.Lock()
	s.running[coord.ID] = auth.PersonID(ctx)
	s.mu.Unlock()
	for n := 1; n <= 2; n++ {
		c, _ := s.chats.Create(ctx, swarm.Label(n, ""), "", "")
		_ = s.chats.SetSwarm(ctx, c.ID, rec.ID, swarm.AgentRole(n))
		rec.Agents = append(rec.Agents, contracts.SwarmAgent{Agent: n, ConversationID: c.ID, Role: swarm.AgentRole(n), Status: swarm.AgentRunning})
		s.mu.Lock()
		s.running[c.ID] = auth.PersonID(ctx)
		s.mu.Unlock()
	}
	return rec, s.st.Save(ctx, rec)
}

func (s *testSwarms) Get(ctx context.Context, id string) (contracts.Swarm, error) {
	rec, err := s.st.Get(ctx, id)
	if err == nil {
		if w := s.tools.WaitingInSwarm(id, auth.PersonID(ctx)); len(w) > 0 {
			rec.Waiting = w
		}
	}
	return rec, err
}

func (s *testSwarms) List(ctx context.Context, projectID string) ([]contracts.Swarm, error) {
	return s.st.List(ctx, projectID)
}

// stopChat stops a running chat of the caller's, as App.StopChatFor does.
func (s *testSwarms) stopChat(ctx context.Context, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if person, ok := s.running[id]; !ok || person != auth.PersonID(ctx) {
		return false
	}
	delete(s.running, id)
	s.stopped = append(s.stopped, id)
	return true
}

func (s *testSwarms) Stop(ctx context.Context, id string) (bool, error) {
	rec, err := s.st.Get(ctx, id)
	if err != nil {
		return false, err
	}
	stopped := s.stopChat(ctx, rec.ConversationID)
	for _, a := range rec.Agents {
		if s.stopChat(ctx, a.ConversationID) {
			stopped = true
		}
	}
	return stopped, nil
}

func (s *testSwarms) Delete(ctx context.Context, id string) error { return s.st.Delete(ctx, id) }

type lsTool struct{}

func (lsTool) ID() string          { return "terminal" }
func (lsTool) DisplayName() string { return "terminal" }
func (lsTool) Description() string { return "runs a command" }
func (lsTool) Execute(context.Context, map[string]any) (map[string]any, error) {
	return map[string]any{"stdout": "ok"}, nil
}

func swarmsServer(t *testing.T) (*Server, *testSwarms, *events.Bus) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	bus := events.NewBus(16)
	reg := tools.NewRegistry(t.TempDir(), bus)
	reg.Register(lsTool{})
	chats := repositories.NewConversationRepo(db.SQL)
	sw := &testSwarms{st: swarm.NewStore(db.SQL), chats: chats, tools: reg, running: map[string]string{}}
	srv := NewServer(Dependencies{
		ListConversations: chats.List,
		StopChat:          sw.stopChat,
		DecideTool:        reg.Decide,
	})
	srv.BindSwarms(sw)
	return srv, sw, bus
}

func started(t *testing.T, body string) contracts.Swarm {
	t.Helper()
	var s contracts.Swarm
	if err := json.Unmarshal([]byte(body), &s); err != nil || s.ID == "" {
		t.Fatalf("not a swarm: %s", body)
	}
	return s
}

// Members and up start swarms; a Visitor, a portal's guest, an unknown mode,
// or a budget out of range can't. A swarm is its starter's alone.
func TestSwarmsAPI(t *testing.T) {
	srv, _, _ := swarmsServer(t)
	h := func(f http.HandlerFunc) http.HandlerFunc { return srv.swarmsReady(f) }
	start := map[string]any{"prompt": "Compare three kayaks", "mode": "race", "budget": map[string]any{"tokens": 50000, "models": []string{"a", "b"}}}
	if code, body := call(h(srv.handleStartSwarm), "vee", auth.RoleVisitor, http.MethodPost, "/", nil, start); code != http.StatusForbidden || !strings.Contains(body, "ROLE_REQUIRED") {
		t.Fatalf("a visitor started one: %d %s", code, body)
	}
	for name, bad := range map[string]map[string]any{
		"no prompt":    {"prompt": " "},
		"unknown mode": {"prompt": "x", "mode": "relay"},
		"budget":       {"prompt": "x", "budget": map[string]any{"tokens": 5}},
	} {
		if code, body := call(h(srv.handleStartSwarm), "vee", auth.RoleMember, http.MethodPost, "/", nil, bad); code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, code, body)
		}
	}
	code, body := call(h(srv.handleStartSwarm), "vee", auth.RoleMember, http.MethodPost, "/", nil, start)
	if code != http.StatusAccepted {
		t.Fatalf("start: %d %s", code, body)
	}
	s := started(t, body)
	if s.Mode != "race" || s.Budget.Tokens != 50000 || s.Budget.Agents != 4 || s.Status != swarm.StatusRunning {
		t.Fatalf("started = %+v", s)
	}
	id := map[string]string{"id": s.ID}
	if code, body := call(h(srv.handleGetSwarm), "vee", auth.RoleMember, http.MethodGet, "/", id, nil); code != http.StatusOK || !strings.Contains(body, `"agents":[{"agent":1`) {
		t.Fatalf("get: %d %s", code, body)
	}
	if code, body := call(h(srv.handleListSwarms), "vee", auth.RoleMember, http.MethodGet, "/api/v1/swarms", nil, nil); code != http.StatusOK || !strings.Contains(body, s.ID) {
		t.Fatalf("list: %d %s", code, body)
	}

	// Another person sees none of it.
	if code, body := call(h(srv.handleListSwarms), "sam", auth.RoleAdmin, http.MethodGet, "/", nil, nil); code != http.StatusOK || body != "[]\n" {
		t.Fatalf("sam's list: %d %s", code, body)
	}
	for name, try := range map[string]func() (int, string){
		"get": func() (int, string) {
			return call(h(srv.handleGetSwarm), "sam", auth.RoleAdmin, http.MethodGet, "/", id, nil)
		},
		"stop": func() (int, string) {
			return call(h(srv.handleStopSwarm), "sam", auth.RoleAdmin, http.MethodPost, "/", id, nil)
		},
		"delete": func() (int, string) {
			return call(h(srv.handleDeleteSwarm), "sam", auth.RoleAdmin, http.MethodDelete, "/", id, nil)
		},
	} {
		if code, body := try(); code != http.StatusNotFound || !strings.Contains(body, "SWARM_NOT_FOUND") {
			t.Errorf("sam's %s: %d %s", name, code, body)
		}
	}
	if code, body := call(h(srv.handleDeleteSwarm), "vee", auth.RoleMember, http.MethodDelete, "/", id, nil); code != http.StatusConflict || !strings.Contains(body, "SWARM_RUNNING") {
		t.Fatalf("deleting a running swarm: %d %s", code, body)
	}

	// A portal's guest can't start one, and neither can a Visitor's chat turn.
	guest := func(f http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			p := auth.PrincipalFrom(r.Context())
			p.Person.PortalID = "shop"
			f(w, r.WithContext(auth.WithPrincipal(r.Context(), p)))
		}
	}
	if code, body := call(guest(h(srv.handleStartSwarm)), "g1", auth.RoleMember, http.MethodPost, "/", nil, start); code != http.StatusForbidden {
		t.Fatalf("a portal's guest started one: %d %s", code, body)
	}
	srv.deps.Chat = func(w http.ResponseWriter, r *http.Request, _, _, _, _ string, _ bool, _ string) error {
		writeJSON(w, http.StatusOK, map[string]string{"swarm": swarm.ModeFrom(r.Context())})
		return nil
	}
	if code, body := call(srv.handleChat, "vee", auth.RoleVisitor, http.MethodPost, "/", nil, map[string]any{"message": "hi", "swarm": "split"}); code != http.StatusForbidden {
		t.Fatalf("a visitor's swarm turn: %d %s", code, body)
	}
	if code, body := call(srv.handleChat, "vee", auth.RoleMember, http.MethodPost, "/", nil, map[string]any{"message": "hi", "swarm": "relay"}); code != http.StatusBadRequest {
		t.Fatalf("an unknown swarm: %d %s", code, body)
	}
	if code, body := call(srv.handleChat, "vee", auth.RoleMember, http.MethodPost, "/", nil, map[string]any{"message": "hi", "swarm": "race"}); code != http.StatusOK || !strings.Contains(body, `"swarm":"race"`) {
		t.Fatalf("a member's swarm turn: %d %s", code, body)
	}
}

// History leaves out agents' chats; swarm_id lists a swarm's, the person's
// own only.
func TestSwarmChatsListed(t *testing.T) {
	srv, _, _ := swarmsServer(t)
	_, body := call(srv.swarmsReady(srv.handleStartSwarm), "vee", auth.RoleMember, http.MethodPost, "/", nil, map[string]any{"prompt": "Plan the garden"})
	s := started(t, body)
	list := func(person, path string) []contracts.Conversation {
		t.Helper()
		code, body := call(srv.handleListConversations, person, auth.RoleMember, http.MethodGet, path, nil, nil)
		if code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, code, body)
		}
		var out []contracts.Conversation
		_ = json.Unmarshal([]byte(body), &out)
		return out
	}
	if got := list("vee", "/api/v1/conversations"); len(got) != 1 || got[0].ID != s.ConversationID || got[0].SwarmID != s.ID {
		t.Fatalf("history = %+v", got)
	}
	got := list("vee", "/api/v1/conversations?swarm_id="+s.ID)
	if len(got) != 3 {
		t.Fatalf("the swarm's chats = %+v", got)
	}
	roles := map[string]bool{}
	for _, c := range got {
		roles[c.SwarmRole] = true
	}
	if !roles[""] || !roles["agent:1"] || !roles["agent:2"] {
		t.Fatalf("roles = %v", roles)
	}
	if got := list("sam", "/api/v1/conversations?swarm_id="+s.ID); len(got) != 0 {
		t.Fatalf("sam lists vee's swarm: %+v", got)
	}
}

// Stopping one agent's chat stops that agent alone; stopping the swarm
// stops every chat in it. Another person stops neither.
func TestSwarmStopAPI(t *testing.T) {
	srv, sw, _ := swarmsServer(t)
	_, body := call(srv.swarmsReady(srv.handleStartSwarm), "vee", auth.RoleMember, http.MethodPost, "/", nil, map[string]any{"prompt": "Plan the garden"})
	s := started(t, body)
	agent2 := s.Agents[1].ConversationID
	if _, body := call(srv.handleStopChat, "sam", auth.RoleAdmin, http.MethodPost, "/", nil, map[string]any{"conversation_id": agent2}); !strings.Contains(body, `"stopped":false`) {
		t.Fatalf("sam stopped vee's agent: %s", body)
	}
	if _, body := call(srv.handleStopChat, "vee", auth.RoleMember, http.MethodPost, "/", nil, map[string]any{"conversation_id": agent2}); !strings.Contains(body, `"stopped":true`) {
		t.Fatalf("stopping one agent: %s", body)
	}
	if len(sw.stopped) != 1 || sw.stopped[0] != agent2 {
		t.Fatalf("stopped %v, want only agent 2", sw.stopped)
	}
	id := map[string]string{"id": s.ID}
	if code, body := call(srv.swarmsReady(srv.handleStopSwarm), "vee", auth.RoleMember, http.MethodPost, "/", id, nil); code != http.StatusOK || !strings.Contains(body, `"stopped":true`) {
		t.Fatalf("stopping the swarm: %d %s", code, body)
	}
	if len(sw.stopped) != 3 || sw.stopped[1] != s.ConversationID || sw.stopped[2] != s.Agents[0].ConversationID {
		t.Fatalf("stopped %v, want the coordinator and agent 1 too", sw.stopped)
	}
}

// An agent's question carries its swarm and agent, the swarm lists it for
// its person alone, and "allow for this session" in one agent's chat
// doesn't let another agent's call through.
func TestSwarmApprovalsAPI(t *testing.T) {
	srv, sw, bus := swarmsServer(t)
	_, body := call(srv.swarmsReady(srv.handleStartSwarm), "vee", auth.RoleMember, http.MethodPost, "/", nil, map[string]any{"prompt": "Plan the garden"})
	s := started(t, body)
	sub, ch := bus.Subscribe()
	defer bus.Unsubscribe(sub)
	vee := auth.WithPrincipal(context.Background(), auth.Principal{Person: auth.Person{ID: "vee", Role: auth.RoleMember}})
	// ask runs a command as agent n, answers its question through the API
	// with allowSession, and reports whether it asked.
	ask := func(n int, allowSession bool) bool {
		t.Helper()
		meta := map[string]any{"conversation_id": s.Agents[n-1].ConversationID, "swarm_id": s.ID, "agent": swarm.Label(n, ""), "agent_number": n}
		errCh := make(chan error, 1)
		go func() {
			_, err := sw.tools.Execute(vee, "terminal", map[string]any{"command": "ls"}, tools.PolicyAsk, "test", meta)
			errCh <- err
		}()
		asked := false
		for {
			select {
			case evt := <-ch:
				if evt.Type != events.ToolRequested {
					continue
				}
				asked = true
				if evt.Payload["swarm_id"] != s.ID || evt.Payload["agent"] != swarm.Label(n, "") {
					t.Errorf("tool.requested = %v", evt.Payload)
				}
				id := map[string]string{"id": s.ID}
				if code, body := call(srv.swarmsReady(srv.handleGetSwarm), "vee", auth.RoleMember, http.MethodGet, "/", id, nil); code != http.StatusOK || !strings.Contains(body, `"waiting":[{`) {
					t.Errorf("vee's swarm doesn't show the question: %d %s", code, body)
				}
				if code, _ := call(srv.swarmsReady(srv.handleGetSwarm), "sam", auth.RoleAdmin, http.MethodGet, "/", id, nil); code != http.StatusNotFound {
					t.Errorf("sam reads vee's swarm: %d", code)
				}
				if code, body := call(srv.handleToolDecide, "vee", auth.RoleMember, http.MethodPost, "/", nil,
					map[string]any{"request_id": evt.Payload["request_id"], "allow": true, "allow_session": allowSession}); code != http.StatusOK {
					t.Fatalf("decide: %d %s", code, body)
				}
			case err := <-errCh:
				if err != nil {
					t.Fatal(err)
				}
				return asked
			case <-time.After(3 * time.Second):
				t.Fatal("timed out")
			}
		}
	}
	if !ask(1, true) {
		t.Fatal("agent 1's first call didn't ask")
	}
	if ask(1, false) {
		t.Error("allowed for the session, agent 1 was asked again")
	}
	if !ask(2, false) {
		t.Error("agent 1's yes let agent 2's call through")
	}
}
