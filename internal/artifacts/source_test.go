package artifacts

import (
	"context"
	"errors"
	"testing"
)

// An automation's camera pictures go with its history when asked (#1335);
// other files stay.
func TestDeleteBySource(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	pic, err := s.Save(ctx, Input{Name: "door.jpg", Producer: "automation", SourceTask: "automation:a1", Data: []byte("jpg")})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.Save(ctx, Input{Name: "yard.jpg", Producer: "automation", SourceTask: "automation:a2", Data: []byte("jpg")})
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.DeleteBySource(ctx, "automation:a1")
	if err != nil || n != 1 {
		t.Fatalf("deleted %d, %v; want 1", n, err)
	}
	if _, err := s.Get(ctx, pic.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the picture stayed: %v", err)
	}
	if _, err := s.Get(ctx, other.ID); err != nil {
		t.Fatalf("another automation's picture went: %v", err)
	}
	if n, err := s.DeleteBySource(ctx, ""); err != nil || n != 0 {
		t.Fatalf("an empty source deleted %d, %v", n, err)
	}
}
