package huginn

import (
	"reflect"
	"strings"
	"testing"
)

func TestMakePlan(t *testing.T) {
	cases := []struct {
		msg  string
		want Plan
		ok   bool
	}{
		{"Compare Ollama, llama.cpp, MLX and vLLM for running models on a Mac",
			Plan{Steps: []string{"Find out about Ollama for running models on a Mac", "Find out about llama.cpp for running models on a Mac", "Find out about MLX for running models on a Mac", "Find out about vLLM for running models on a Mac"}, Parallel: true, NeedsWeb: true}, true},
		{"Research the Synology DS224+ vs the QNAP TS-264",
			Plan{Steps: []string{"Find out about Synology DS224+", "Find out about QNAP TS-264"}, Parallel: true, NeedsWeb: true}, true},
		{"Find three NAS drives, then compare price per TB, then create a spreadsheet",
			Plan{Steps: []string{"Find three NAS drives", "compare price per TB", "create a spreadsheet"}, NeedsWeb: true}, true},
		{"Plan my week:\n1. Draft the report\n2. Book flights\n3. Email the team about Friday", Plan{
			Steps: []string{"Plan my week", "Draft the report", "Book flights", "Email the team about Friday"}}, true},
		{"What is DNS?", Plan{}, false},
		{"Tell me a story about a fox who learns to sail across the sea", Plan{}, false},
		{"Compare this sentence, which has commas, with a longer rambling thought that goes on and on", Plan{}, false},
	}
	for _, c := range cases {
		got, ok := MakePlan(c.msg)
		if ok != c.ok || !reflect.DeepEqual(got, c.want) {
			t.Errorf("MakePlan(%q) = %+v %v, want %+v %v", c.msg, got, ok, c.want, c.ok)
		}
	}
}

func TestCheck(t *testing.T) {
	evidence := "item: Michelin Defender; price: 176.50; in stock: 4\nitem: WD Red 8TB; price: 420; capacity TB: 8"
	// Grounded figures, rounding, and figures derived from the sources pass.
	ok := "The Michelin Defender is $176.50 (about $177) with 4 in stock. The WD Red is 420 / 8 = 52.5 per TB, or $52.50 per TB."
	if issues := Check(ok, evidence, ""); len(issues) != 0 {
		t.Fatalf("issues for a correct answer: %+v", issues)
	}
	// The mistake the 1B model made: a stock count that is not in the data.
	bad := "The Michelin Defender costs $176.50 and there are 20 units available."
	issues := Check(bad, evidence, "")
	if len(issues) != 1 || issues[0].Kind != "unsupported" || issues[0].Text != "20" {
		t.Fatalf("issues = %+v", issues)
	}
	// Arithmetic is checked even without sources.
	issues = Check("17 × 23 = 381, and 10 + 5 = 15.", "", "")
	if len(issues) != 1 || issues[0].Kind != "arithmetic" || issues[0].Want != "391" {
		t.Fatalf("arithmetic = %+v", issues)
	}
	// Without sources, figures are not judged.
	if issues := Check("There are 20 units.", "", ""); len(issues) != 0 {
		t.Fatalf("no evidence = %+v", issues)
	}
	// Years and list numbers are not claims about the data.
	if issues := Check("In 2026 there were 3 options.", evidence, ""); len(issues) != 0 {
		t.Fatalf("years = %+v", issues)
	}
	if Describe(nil) != "" || !strings.Contains(Describe(issues), "the result is 391") || Figures([]Issue{{Text: "20"}, {Text: "$9"}}) != "20 and $9" {
		t.Fatal("describe and figures")
	}
}

func TestCheckUsesTheLinesAboutTheSubject(t *testing.T) {
	evidence := "item: Michelin Defender; size: 225/45R17; price: 176.50; in stock: 4\n" +
		"item: Bridgestone Turanza; size: 215/55R16; price: 142.50; in stock: 18\n" +
		"item: Goodyear Assurance; size: 205/60R16; price: 129.00; in stock: 9"
	// 18 is in the data, but for another tire.
	issues := Check("The Michelin Defender costs $176.50, and there are 18 Michelin Defender tires in stock.", evidence, "")
	if len(issues) != 1 || issues[0].Text != "18" {
		t.Fatalf("issues = %+v", issues)
	}
	if issues := Check("The Bridgestone Turanza costs $142.50 with 18 in stock.", evidence, ""); len(issues) != 0 {
		t.Fatalf("a correct sentence was flagged: %+v", issues)
	}
}

func TestCheckIgnoresDates(t *testing.T) {
	evidence := "item: Michelin Defender; price: 176.50; in stock: 4"
	issues := Check("As of September 28, 2026, there are 20 Michelin Defender tires, updated 2026-09-28.", evidence, "")
	if len(issues) != 1 || issues[0].Text != "20" {
		t.Fatalf("issues = %+v", issues)
	}
	if got := numberRe.FindAllString("2026, and $1,249.50, ok", -1); !reflect.DeepEqual(got, []string{"2026", "$1,249.50"}) {
		t.Fatalf("numbers = %q", got)
	}
}
