package taxonomy

import (
	"strings"
	"testing"
)

func TestBuiltin(t *testing.T) {
	tax := Builtin()
	for _, id := range []string{"chat_coding", "security", "retrieval", "documents", "vision", "speech", "creative", "science", "agents"} {
		if _, ok := tax.category(id); !ok {
			t.Errorf("no %s category", id)
		}
	}
	if c, _ := tax.category("security"); !strings.Contains(strings.Join(c.Tasks, " "), "decompilation") {
		t.Errorf("security tasks %v", c.Tasks)
	}
}

func TestParseRefusesDuplicates(t *testing.T) {
	for name, raw := range map[string]string{
		"task in two categories": `{"version":1,"modalities":["text"],"categories":[{"id":"a","tasks":["x"]},{"id":"b","tasks":["x"]}]}`,
		"uncategorized":          `{"version":1,"modalities":["text"],"categories":[{"id":"uncategorized","tasks":["x"]}]}`,
		"no version":             `{"modalities":["text"],"categories":[{"id":"a","tasks":["x"]}]}`,
	} {
		if _, err := Parse([]byte(raw)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestCheck(t *testing.T) {
	tax := Builtin()
	fine := Profile{PrimaryCategory: "speech", Categories: []string{"speech"}, Tasks: []string{"transcription"}, InputModalities: []string{"audio"}, OutputModalities: []string{"text"}}
	if err := tax.Check(fine); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(p *Profile){
		"primary not among categories": func(p *Profile) { p.PrimaryCategory = "vision" },
		"unknown category":             func(p *Profile) { p.Categories = append(p.Categories, "astrology") },
		"task from another category":   func(p *Profile) { p.Tasks = []string{"decompilation"} },
		"unknown modality":             func(p *Profile) { p.OutputModalities = []string{"smell"} },
		"no tasks":                     func(p *Profile) { p.Tasks = nil },
		"twice":                        func(p *Profile) { p.Tasks = []string{"transcription", "transcription"} },
	}
	for name, change := range cases {
		p := fine
		p.Categories = append([]string(nil), fine.Categories...)
		change(&p)
		if err := tax.Check(p); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if got := (Profile{}).OrUncategorized(); got.PrimaryCategory != Uncategorized {
		t.Errorf("empty profile is %q", got.PrimaryCategory)
	}
}
