package muninn

import (
	"testing"
)

// What a project's chats learn is recalled in its chats, beside the
// person's own memory, and nowhere else (#1136).
func TestProjectMemory(t *testing.T) {
	s := newStore(t)
	ctx := as("ana")
	if _, _, err := s.Add(ctx, "I water the garden on Sundays", "", SourceExplicit, ""); err != nil {
		t.Fatal(err)
	}
	learned, created, err := s.AddIn(ctx, "garden", "The garden soil is clay", "", SourceExplicit, "c1")
	if err != nil || !created || learned.ProjectID != "garden" {
		t.Fatalf("added %+v %v %v", learned, created, err)
	}
	if _, _, err := s.AddIn(ctx, "taxes", "The tax soil is a receipt box", "", SourceExplicit, "c2"); err != nil {
		t.Fatal(err)
	}
	has := func(list []Memory, content string) bool {
		for _, m := range list {
			if m.Content == content {
				return true
			}
		}
		return false
	}
	in, _ := s.Relevant(ctx, "what soil is the garden?", "garden")
	if !has(in, "The garden soil is clay") || !has(in, "I water the garden on Sundays") || has(in, "The tax soil is a receipt box") {
		t.Fatalf("in the project: %+v", in)
	}
	out, _ := s.Relevant(ctx, "what soil is the garden?", "")
	if has(out, "The garden soil is clay") || !has(out, "I water the garden on Sundays") {
		t.Fatalf("outside it: %+v", out)
	}
	// The same thing learned again isn't a copy; in another project it's new.
	if _, created, _ := s.AddIn(ctx, "garden", "the garden soil is clay.", "", SourceExplicit, "c1"); created {
		t.Fatal("added a copy")
	}
	if _, created, _ := s.AddIn(ctx, "taxes", "The garden soil is clay", "", SourceExplicit, "c2"); !created {
		t.Fatal("another project's memory counted as a copy")
	}
	if err := s.DeleteProject(as("sam"), "garden"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteProject(ctx, "garden"); err != nil {
		t.Fatal(err)
	}
	all, _ := s.List(ctx)
	if len(all) != 3 {
		t.Fatalf("left %+v", all)
	}
	for _, m := range all {
		if m.ProjectID == "garden" {
			t.Fatalf("the project's memory outlived it: %+v", m)
		}
	}
}

// "Forget that …" finds what the chat recalls: in a project, the person's
// own memories and the project's; outside one, only the person's.
func TestForgetIsScopedLikeRecall(t *testing.T) {
	s := newStore(t)
	ctx := as("ana")
	garden, _, err := s.AddIn(ctx, "garden", "The garden soil is clay", "", SourceExplicit, "")
	if err != nil {
		t.Fatal(err)
	}
	mine, _, err := s.Add(ctx, "I water on Sundays", "", SourceExplicit, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"The garden soil is clay", "garden soil clay"} {
		if m, ok, _ := s.Find(ctx, text, "taxes"); ok {
			t.Errorf("%q in another project found %+v", text, m)
		}
		if m, ok, _ := s.Find(ctx, text, ""); ok {
			t.Errorf("%q outside a project found %+v", text, m)
		}
		if m, ok, err := s.Find(ctx, text, "garden"); !ok || err != nil || m.ID != garden.ID {
			t.Errorf("%q in the project: %+v %v %v", text, m, ok, err)
		}
	}
	if m, ok, _ := s.Find(ctx, "water Sundays", "garden"); !ok || m.ID != mine.ID {
		t.Fatalf("the person's own memory in a project: %+v %v", m, ok)
	}
}
