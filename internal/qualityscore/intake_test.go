package qualityscore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var (
	intakeNow  = time.Date(2026, 11, 3, 22, 0, 0, 0, time.UTC)
	intakeList = []ListedModel{{ID: "qwen2.5-3b-q4", Name: "Qwen 2.5 3B"}}
)

func goodRow() Row {
	return Row{
		Model: "qwen2.5-3b-q4", Name: "Qwen 2.5 3B", Date: "2026-11-03", Hardware: "iPhone 16 Pro Max, 8 GB", OS: "iOS 27.2",
		MethodVersion: 1, Submitted: true, Runs: RunsFull, Scores: map[string]Passed{},
		Performance: &Performance{PromptTokens: 4269, PromptTokensPerSecond: 187.3, TokensPerSecond: 21.7, FirstTokenMs: 22928, EffectiveTokensPerSecond: 2.6, GPULayers: 37, GPULayersTotal: 37},
	}
}

func TestCheckPassesRealResults(t *testing.T) {
	if why := Check(goodRow(), intakeList, nil, 1, intakeNow); len(why) != 0 {
		t.Errorf("a phone's real result: %v", why)
	}
	full := goodRow()
	full.Hardware, full.OS = "AMD RX 7900 XTX, 24 GB", "Linux"
	full.Performance = &Performance{PromptTokens: 4269, PromptTokensPerSecond: 5444.6, TokensPerSecond: 248.4, FirstTokenMs: 861, EffectiveTokensPerSecond: 60, GPULayers: 37, GPULayersTotal: 37}
	full.Scores = map[string]Passed{"quality": {62, 64}, "topics": {18, 22}, "files": {17, 21}, "reasoning": {30, 39}}
	if why := Check(full, intakeList, nil, 1, intakeNow); len(why) != 0 {
		t.Errorf("a computer's real result: %v", why)
	}
	no := Row{Model: "qwen2.5-3b-q4", Name: "Qwen 2.5 3B", Date: "2026-11-03", Hardware: "Pixel 6, 8 GB", OS: "Android 16", MethodVersion: 1, Runs: RunsNo, Why: "out_of_memory"}
	if why := Check(no, intakeList, nil, 1, intakeNow); len(why) != 0 {
		t.Errorf("a model that doesn't run: %v", why)
	}
}

func TestCheckHoldsWhatsUnusual(t *testing.T) {
	for name, change := range map[string]func(*Row){
		"not on the list":       func(r *Row) { r.Model = "made-up" },
		"off the list":          func(r *Row) { r.OffList = true },
		"rude computer":         func(r *Row) { r.Hardware = "Buy my crypto, 8 GB" },
		"link as a computer":    func(r *Row) { r.Hardware = "Apple https://evil.example, 8 GB" },
		"odd OS":                func(r *Row) { r.OS = "BuyMyOS" },
		"old method":            func(r *Row) { r.MethodVersion = 0 },
		"old date":              func(r *Row) { r.Date = "2025-01-01" },
		"future date":           func(r *Row) { r.Date = "2027-01-01" },
		"own source":            func(r *Row) { r.Source = "https://evil.example" },
		"unknown runs":          func(r *Row) { r.Runs = "great" },
		"impossible speed":      func(r *Row) { r.Performance.TokensPerSecond = 1000000 },
		"numbers don't agree":   func(r *Row) { r.Performance.FirstTokenMs = 1 },
		"too many layers":       func(r *Row) { r.Performance.GPULayers = 99 },
		"faster than writing":   func(r *Row) { r.Performance.EffectiveTokensPerSecond = 500 },
		"full, with layers off": func(r *Row) { r.Performance.GPULayers = 5 },
		"some of the sets":      func(r *Row) { r.Scores = map[string]Passed{"quality": {60, 64}} },
		"wrong size of set": func(r *Row) {
			r.Scores = map[string]Passed{"quality": {5, 5}, "topics": {5, 5}, "files": {5, 5}, "reasoning": {5, 5}}
		},
		"no figures": func(r *Row) { r.Performance = nil },
	} {
		row := goodRow()
		change(&row)
		if why := Check(row, intakeList, nil, 1, intakeNow); len(why) == 0 {
			t.Errorf("%s: passed", name)
		}
	}
}

func TestCheckHoldsRepeatsAndFloods(t *testing.T) {
	if why := Check(goodRow(), intakeList, []Row{goodRow()}, 1, intakeNow); len(why) == 0 {
		t.Error("the same result again passed")
	}
	var today []Row
	for range MaxAutoPerDay {
		other := goodRow()
		other.Model = "other"
		today = append(today, other)
	}
	if why := Check(goodRow(), intakeList, today, 1, intakeNow); len(why) == 0 {
		t.Error("past the day's limit passed")
	}
}

func TestIntakeReadsIssuesAndChecksCredit(t *testing.T) {
	body := "Sent from the benchmark.\n\n```json\n" + `{"model":"qwen2.5-3b-q4","name":"Qwen 2.5 3B","date":"2026-11-03","hardware":"iPhone 16 Pro Max, 8 GB","os":"iOS 27.2","method_version":1,"runs":"full","submitted_by":"someone-else","scores":{},"performance":{"prompt_tokens":4269,"prompt_tokens_per_second":187.3,"tokens_per_second":21.7,"first_token_ms":22928,"effective_tokens_per_second":2.6,"gpu_layers":37,"gpu_layers_total":37}}` + "\n```\n"
	got := Intake(body, "app/toskar-reports", "https://github.com/o/r/issues/1", intakeList, nil, 1, intakeNow)
	if !got.Passed || len(got.Rows) != 1 {
		t.Fatalf("%+v", got)
	}
	row := got.Rows[0]
	if row.SubmittedBy != "" || !row.Submitted || row.Source != "https://github.com/o/r/issues/1" {
		t.Errorf("%+v: someone else's name must not stick", row)
	}
	mine := Intake(strings.ReplaceAll(body, "someone-else", "octo-cat"), "Octo-Cat", "s", intakeList, nil, 1, intakeNow)
	if mine.Rows[0].SubmittedBy != "octo-cat" {
		t.Errorf("%+v: who opened the issue keeps their credit", mine.Rows[0])
	}
	// The name shown is the list's, whatever was sent: an id, as an app that
	// didn't know the name yet sent (#1087), or words of the sender's own.
	for _, sent := range []string{"qwen2.5-3b-q4", "Best Model Ever", ""} {
		v := Intake(strings.Replace(body, `"name":"Qwen 2.5 3B"`, `"name":"`+sent+`"`, 1), "a", "s", intakeList, nil, 1, intakeNow)
		if !v.Passed || v.Rows[0].Name != "Qwen 2.5 3B" {
			t.Errorf("sent %q: %+v", sent, v)
		}
	}
	// A run's results come as a list, and one bad row holds them all.
	many := "```json\n[" + strings.Split(strings.Split(body, "```json\n")[1], "\n```")[0] + `,{"model":"made-up","name":"x"}]` + "\n```"
	if v := Intake(many, "a", "s", intakeList, nil, 1, intakeNow); v.Passed || len(v.Why) == 0 {
		t.Errorf("%+v", v)
	}
	if v := Intake("no block here", "a", "s", intakeList, nil, 1, intakeNow); v.Passed {
		t.Errorf("%+v", v)
	}
	if v := Intake("```json\n{not json\n```", "a", "s", intakeList, nil, 1, intakeNow); v.Passed {
		t.Errorf("%+v", v)
	}
}

func TestReasonsCarryNoMentionsOrLinks(t *testing.T) {
	got := plainReason(`@everyone: the name "[x](https://evil.example) <img>" isn't one`)
	for _, bad := range []string{"@", "[", "]", "<", ">", "https"} {
		if strings.Contains(got, bad) {
			t.Errorf("%q carries %q", got, bad)
		}
	}
	if len(plainReason(strings.Repeat("x", 1000))) > 300 {
		t.Error("too long")
	}
}

func TestAddRowsLeavesTheRestAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "results.json")
	before := "{\n  \"results\": [\n    {\"model\": \"old\"}\n  ]\n}\n"
	if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AddRows(path, []Row{goodRow()}); err != nil {
		t.Fatal(err)
	}
	rows, err := ReadResults(path)
	if err != nil || len(rows) != 2 || rows[1].Hardware != "iPhone 16 Pro Max, 8 GB" {
		t.Errorf("%v %+v", err, rows)
	}
}
