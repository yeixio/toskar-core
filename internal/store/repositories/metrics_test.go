package repositories_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/internal/store/repositories"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestMetricsInsertListRoleSteps(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	repo := repositories.NewMetricsRepo(db.SQL)
	ctx := context.Background()
	_, err = repo.Insert(ctx, contracts.GenerationRun{
		ConversationTitle: "Team demo",
		ProfileName:       "Programming",
		ModelID:           "m1",
		TotalMs:           900,
		EvalTokPerSec:     40,
		RoleSteps: []contracts.GenerationRoleStep{
			{Role: "coordinator", NodeID: "a", NodeName: "Desktop", ModelID: "m1", TotalMs: 300, EvalTokPerSec: 50, CompletionTokens: 20},
			{Role: "worker", NodeID: "b", NodeName: "Laptop", ModelID: "m1", TotalMs: 400, EvalTokPerSec: 35, CompletionTokens: 40},
			{Role: "reviewer", NodeID: "a", NodeName: "Desktop", ModelID: "m1", TotalMs: 200, EvalTokPerSec: 45, CompletionTokens: 15},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	items, err := repo.List(ctx, repositories.ListFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("len=%d", len(items))
	}
	run := items[0]
	if !run.CrossMachine {
		t.Fatal("expected cross_machine")
	}
	if run.NodeCount != 2 {
		t.Fatalf("node_count=%d", run.NodeCount)
	}
	if len(run.RoleSteps) != 3 {
		t.Fatalf("role_steps=%d", len(run.RoleSteps))
	}
	if run.RoleSteps[1].NodeName != "Laptop" {
		t.Fatalf("worker node=%q", run.RoleSteps[1].NodeName)
	}
}

// A reply keeps the backend and device that produced it (#317); one
// recorded without them reads back empty.
func TestMetricsKeepTheDevice(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := repositories.NewMetricsRepo(db.SQL)
	ctx := context.Background()
	if _, err := repo.Insert(ctx, contracts.GenerationRun{ModelID: "gpu", Backend: "vulkan", Device: "AMD Radeon RX 7900 XTX"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Insert(ctx, contracts.GenerationRun{ModelID: "unknown"}); err != nil {
		t.Fatal(err)
	}
	items, err := repo.List(ctx, repositories.ListFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]contracts.GenerationRun{}
	for _, it := range items {
		got[it.ModelID] = it
	}
	if g := got["gpu"]; g.Backend != "vulkan" || g.Device != "AMD Radeon RX 7900 XTX" {
		t.Errorf("gpu reply: %q %q", g.Backend, g.Device)
	}
	if u := got["unknown"]; u.Backend != "" || u.Device != "" {
		t.Errorf("unknown reply: %q %q", u.Backend, u.Device)
	}
}

// A reply keeps what speculative decoding guessed ahead and how much of it
// the model kept (#835); one without speculation reads back zero.
func TestMetricsKeepTheDrafts(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := repositories.NewMetricsRepo(db.SQL)
	ctx := context.Background()
	if _, err := repo.Insert(ctx, contracts.GenerationRun{ModelID: "guessed", DraftTokens: 120, DraftAccepted: 42}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Insert(ctx, contracts.GenerationRun{ModelID: "plain"}); err != nil {
		t.Fatal(err)
	}
	items, err := repo.List(ctx, repositories.ListFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]contracts.GenerationRun{}
	for _, it := range items {
		got[it.ModelID] = it
	}
	if g := got["guessed"]; g.DraftTokens != 120 || g.DraftAccepted != 42 {
		t.Errorf("guessed reply: %d %d", g.DraftTokens, g.DraftAccepted)
	}
	if p := got["plain"]; p.DraftTokens != 0 || p.DraftAccepted != 0 {
		t.Errorf("plain reply: %d %d", p.DraftTokens, p.DraftAccepted)
	}
}
