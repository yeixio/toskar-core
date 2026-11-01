package turnopts

import (
	"context"
	"testing"
)

func TestFrom(t *testing.T) {
	if o := From(context.Background()); o != nil {
		t.Fatalf("From on a plain context = %+v, want nil", o)
	}
	want := &Options{Memory: true, Language: "es"}
	if got := From(With(context.Background(), want)); got != want {
		t.Fatalf("From = %p, want the pointer With stored, %p", got, want)
	}
}

func TestBranchFrom(t *testing.T) {
	b := BranchFrom(context.Background())
	if b != (Branch{}) || b.Any() {
		t.Fatalf("BranchFrom on a plain context = %+v (Any %v), want the zero Branch", b, b.Any())
	}
	want := Branch{RetryOf: "m2"}
	if got := BranchFrom(WithBranch(context.Background(), want)); got != want {
		t.Fatalf("BranchFrom = %+v, want %+v", got, want)
	}
}

func TestBranchAny(t *testing.T) {
	for _, tc := range []struct {
		name string
		b    Branch
		want bool
	}{
		{"end", Branch{}, false},
		{"after a message", Branch{ParentID: "m1"}, true},
		{"retry", Branch{RetryOf: "m2"}, true},
		{"edit", Branch{EditOf: "m3"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.b.Any(); got != tc.want {
				t.Fatalf("Any() = %v, want %v", got, tc.want)
			}
		})
	}
}

// Options and a Branch ride on the same context without one hiding the other.
func TestKeysApart(t *testing.T) {
	onlyBranch := WithBranch(context.Background(), Branch{ParentID: "m1"})
	if o := From(onlyBranch); o != nil {
		t.Fatalf("From on a context with only a Branch = %+v, want nil", o)
	}
	onlyOptions := With(context.Background(), &Options{})
	if b := BranchFrom(onlyOptions); b.Any() {
		t.Fatalf("BranchFrom on a context with only Options = %+v, want the zero Branch", b)
	}
	o := &Options{System: "be brief"}
	both := WithBranch(With(context.Background(), o), Branch{EditOf: "m3"})
	if From(both) != o || BranchFrom(both).EditOf != "m3" {
		t.Fatalf("with both, From = %+v and BranchFrom = %+v", From(both), BranchFrom(both))
	}
}
