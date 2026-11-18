package profiles

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/yeixio/toskar-core/internal/store"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A profile that talks with strangers keeps saying so (#667).
func TestAudienceIsStored(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	m := NewManager(db.SQL)
	ctx := context.Background()
	p, err := m.Create(ctx, Profile{Name: "Front door", OrchestratorID: "simple", Audience: " Public "})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := m.Get(ctx, p.ID); got.Audience != contracts.AudiencePublic {
		t.Fatalf("audience = %q", got.Audience)
	}
	p.Audience = ""
	if err := m.Update(ctx, p); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.Get(ctx, p.ID); got.Audience != "" {
		t.Fatalf("audience after clearing = %q", got.Audience)
	}
	if _, err := m.Create(ctx, Profile{Name: "Odd", OrchestratorID: "simple", Audience: "everyone"}); err == nil {
		t.Fatal("an unknown audience was accepted")
	}
}
