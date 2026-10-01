package profiles

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/yggdrasil-core/internal/store"
	"github.com/yeixio/yggdrasil-core/pkg/contracts"
)

func TestOrchestrationControls(t *testing.T) {
	for _, bad := range []contracts.OrchestrationPolicy{
		{Effort: "maximum"}, {Planning: "maybe"}, {Verification: "twice"}, {MaxWorkers: 1}, {MaxWorkers: 20},
		{MaxToolCalls: 99}, {TimeoutSeconds: 5}, {ContextShare: 0.95}, {Memory: "on"},
	} {
		if err := ValidateOrchestration(bad); err == nil || !strings.HasPrefix(err.Error(), "orchestration.") {
			t.Errorf("%+v accepted (%v)", bad, err)
		}
	}
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	m := NewManager(db.SQL)
	ctx := context.Background()
	o := contracts.OrchestrationPolicy{Effort: "thorough", Planning: "on", MaxWorkers: 4, Parallel: "off", Verification: "correct",
		MaxToolCalls: 6, Memory: "off", ContextShare: 0.3, Fallback: "off", TimeoutSeconds: 120}
	p, err := m.Create(ctx, Profile{Name: "Careful", OrchestratorID: "simple", Orchestration: o})
	if err != nil {
		t.Fatal(err)
	}
	if p.Orchestration != o {
		t.Fatalf("stored %+v", p.Orchestration)
	}
	p.Orchestration = contracts.OrchestrationPolicy{}
	if err := m.Update(ctx, p); err != nil {
		t.Fatal(err)
	}
	got, _ := m.Get(ctx, p.ID)
	if got.Orchestration != (contracts.OrchestrationPolicy{}) {
		t.Fatalf("cleared = %+v", got.Orchestration)
	}
	if _, err := m.Create(ctx, Profile{Name: "Bad", OrchestratorID: "simple", Orchestration: contracts.OrchestrationPolicy{Effort: "max"}}); err == nil {
		t.Fatal("invalid controls saved")
	}
}
