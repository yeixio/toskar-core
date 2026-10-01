package mimir

import (
	"strings"
	"testing"
)

func TestFilePassagesReadsEachKind(t *testing.T) {
	p, err := FilePassages("prices.csv", []byte("item,price\nTire,189.99\nWiper,12\n"))
	if err != nil || len(p) != 2 || p[1].Body != "item: Wiper; price: 12" {
		t.Fatalf("csv = %+v %v", p, err)
	}
	p, err = FilePassages("main.go", []byte("package main\n\nfunc main() {}\n"))
	if err != nil || len(p) != 1 || !strings.Contains(p[0].Body, "func main") {
		t.Fatalf("code = %+v %v", p, err)
	}
	if _, err := FilePassages("photo.png", []byte{0x89, 'P', 'N', 'G'}); err == nil {
		t.Fatal("an image is not readable yet")
	}
	if !Attachable("Report.PDF") || Attachable("movie.mp4") {
		t.Fatal("attachable extensions")
	}
}

func TestSelectPassages(t *testing.T) {
	ps := []Passage{
		{Title: "a", Body: strings.Repeat("intro ", 20)},
		{Title: "b", Body: "The warranty covers tread wear for 60,000 miles."},
		{Title: "c", Body: strings.Repeat("filler ", 20)},
		{Title: "d", Body: "Returns are accepted within 30 days."},
	}
	if got := SelectPassages(ps, "anything", 10_000, true); len(got) != 4 {
		t.Fatal("everything fits, so everything is kept")
	}
	got := SelectPassages(ps, "How long is the tread warranty?", 120, false)
	if len(got) != 2 || got[0].Title != "a" && got[0].Title != "b" {
		t.Fatalf("got %+v", got)
	}
	found := false
	for _, p := range got {
		if p.Title == "b" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the matching passage was dropped: %+v", got)
	}
	// No question: the start of the file.
	if got := SelectPassages(ps, "", 125, false); len(got) == 0 || got[0].Title != "a" {
		t.Fatalf("no question = %+v", got)
	}
	// Earlier files add only what matches.
	got = SelectPassages(ps, "returns policy", 60, true)
	if len(got) != 1 || got[0].Title != "d" {
		t.Fatalf("matching only = %+v", got)
	}
	if got := SelectPassages(ps, "zebra", 60, true); len(got) != 0 {
		t.Fatalf("nothing matches = %+v", got)
	}
}
