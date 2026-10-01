package mimir

import (
	"context"
	"testing"
)

func TestSourceThisComputerOnly(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	src, err := s.Create(ctx, CreateInput{Kind: KindText, Filename: "salaries.csv", Text: "name,salary\nAda,100\n"})
	if err != nil || src.LocalOnly {
		t.Fatalf("created %+v, %v", src, err)
	}
	on := true
	if src, err = s.Update(ctx, src.ID, UpdateInput{LocalOnly: &on}); err != nil || !src.LocalOnly {
		t.Fatalf("update = %+v, %v", src, err)
	}
	list, _ := s.List(ctx)
	if len(list) != 1 || !list[0].LocalOnly {
		t.Fatalf("list = %+v", list)
	}
}
