package swarm

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/store/repositories"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

func as(person string) context.Context {
	return auth.WithPrincipal(context.Background(), auth.Principal{Person: auth.Person{ID: person, Role: auth.RoleMember}})
}

// A swarm is its starter's: another person can't see, change, or delete it.
// Deleting one keeps its chats, as ordinary chats.
func TestStore(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	st := NewStore(db.SQL)
	chats := repositories.NewConversationRepo(db.SQL)
	vee, sam := as("vee"), as("sam")

	coord, _ := chats.Create(vee, "Kayaks", "", "")
	s, err := st.Create(vee, "p1", coord.ID, ModeSplit, "Which kayak?", contracts.SwarmBudget{Agents: 4, Tokens: 400_000, Minutes: 5})
	if err != nil {
		t.Fatal(err)
	}
	agent, _ := chats.Create(vee, "Agent 1: prices", "", "")
	if err := chats.SetSwarm(vee, agent.ID, s.ID, AgentRole(1)); err != nil {
		t.Fatal(err)
	}
	if err := chats.SetSwarm(sam, agent.ID, "theirs", AgentRole(1)); err == nil {
		t.Fatal("sam marked vee's chat")
	}
	got, _ := chats.Get(vee, agent.ID)
	if got.SwarmID != s.ID || got.SwarmRole != "agent:1" {
		t.Fatalf("agent chat = %+v", got)
	}

	s.Agents = []contracts.SwarmAgent{{Agent: 1, ConversationID: agent.ID, Role: "agent:1", Part: "prices", Status: AgentRunning}}
	s.Tokens = 120
	if err := st.Save(vee, s); err != nil {
		t.Fatal(err)
	}
	if err := st.Save(sam, s); !errors.Is(err, ErrNotFound) {
		t.Fatalf("sam's save: %v", err)
	}
	if _, err := st.Get(sam, s.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("sam's get: %v", err)
	}
	if list, _ := st.List(sam, ""); len(list) != 0 {
		t.Fatalf("sam's list: %+v", list)
	}
	if list, _ := st.List(vee, "p1"); len(list) != 1 || list[0].Tokens != 120 || len(list[0].Agents) != 1 || list[0].Budget.Agents != 4 {
		t.Fatalf("vee's list: %+v", list)
	}
	if list, _ := st.List(vee, "p2"); len(list) != 0 {
		t.Fatalf("another project's list: %+v", list)
	}
	if err := st.Delete(vee, s.ID); !errors.Is(err, ErrRunning) {
		t.Fatalf("deleting a running swarm: %v", err)
	}
	s.Status, s.Winner = StatusDone, 1
	if err := st.Save(vee, s); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Get(vee, s.ID); got.FinishedAt == nil || got.Winner != 1 {
		t.Fatalf("finished = %+v", got)
	}
	if err := st.Delete(sam, s.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("sam's delete: %v", err)
	}
	if err := st.Delete(vee, s.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := chats.Get(vee, agent.ID); err != nil || got.SwarmID != "" || got.SwarmRole != "" {
		t.Fatalf("the agent's chat after delete: %+v, %v", got, err)
	}

	// A swarm left running when Core stopped is stopped.
	left, _ := st.Create(vee, "", coord.ID, ModeRace, "?", contracts.SwarmBudget{})
	if err := st.FailRunning(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Get(vee, left.ID); got.Status != StatusStopped {
		t.Fatalf("left running: %s", got.Status)
	}
}

// A task swarm's outcome is kept, and the cleanup finds finished swarms past
// their week that still have branches, then forgets them.
func TestStoreKeptBranches(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	st := NewStore(db.SQL)
	vee := as("vee")
	clock := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	st.now = func() time.Time { return clock }
	old, _ := st.Create(vee, "", "c1", ModeSplit, "Fix it.", contracts.SwarmBudget{})
	old.Status, old.Folder, old.Branch, old.Checks, old.Applied = StatusDone, "/src/app", "toskar/swarm/x/merged", ChecksPassed, true
	old.Agents = []contracts.SwarmAgent{{Agent: 1, Branch: "toskar/swarm/x/agent-1", Paths: []string{"a.go"}, Checks: ChecksFailed}}
	old.Conflicts = []contracts.SwarmConflict{{Agent: 2, Branch: "toskar/swarm/x/agent-2", Files: []string{"a.go"}}}
	if err := st.Save(vee, old); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(vee, old.ID)
	if got.Folder != "/src/app" || got.Branch != old.Branch || got.Checks != ChecksPassed || !got.Applied || len(got.Conflicts) != 1 ||
		got.Agents[0].Paths[0] != "a.go" || got.Agents[0].Checks != ChecksFailed {
		t.Fatalf("saved = %+v", got)
	}
	clock = clock.Add(10 * 24 * time.Hour)
	question, _ := st.Create(vee, "", "c2", ModeSplit, "Which kayak?", contracts.SwarmBudget{})
	question.Status = StatusDone
	_ = st.Save(vee, question)
	kept, err := st.KeptBranches(context.Background(), clock.Add(-7*24*time.Hour))
	if err != nil || len(kept) != 1 || kept[0].ID != old.ID {
		t.Fatalf("kept = %+v, %v", kept, err)
	}
	if err := st.ForgetBranches(context.Background(), old.ID); err != nil {
		t.Fatal(err)
	}
	if kept, _ := st.KeptBranches(context.Background(), clock); len(kept) != 0 {
		t.Fatalf("still kept: %+v", kept)
	}
	if got, _ := st.Get(vee, old.ID); got.Branch != "" || got.Agents[0].Branch != "" || got.Folder != "/src/app" {
		t.Errorf("forgotten = %+v", got)
	}
}
