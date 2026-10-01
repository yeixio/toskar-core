package muninn

import (
	"context"
	"testing"
)

func TestMemoryThisComputerOnly(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	m, _, err := s.Add(ctx, "My salary is confidential and I work at Acme", "", SourceManual, "")
	if err != nil || m.LocalOnly {
		t.Fatalf("added %+v, %v", m, err)
	}
	on := true
	if m, err = s.Update(ctx, m.ID, Patch{LocalOnly: &on}); err != nil || !m.LocalOnly {
		t.Fatalf("update = %+v, %v", m, err)
	}
	rel, err := s.Relevant(ctx, "Where do I work?")
	if err != nil || len(rel) == 0 || !rel[0].LocalOnly {
		t.Fatalf("relevant = %+v, %v", rel, err)
	}
}
