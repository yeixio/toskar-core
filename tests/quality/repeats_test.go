package quality

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/yeixio/toskar-core/internal/config"
)

// Running the set more than once (#834). A real model's answers vary, so
// one run per case can swing the pass rate by a case either way:
//
//	TOSKAR_QUALITY_REPEATS=3        runs each case three times, with seeds 1–3
//	TOSKAR_QUALITY_TEMPERATURE=0.7  how a served model samples; 0.7 when
//	                                repeating, 0 (greedy) for one run
//	TOSKAR_QUALITY_SAVE=runs.jsonl  saves every run's answer and what it did
//	TOSKAR_QUALITY_RESCORE=runs.jsonl
//	                                checks saved runs again, without asking
//	                                a model, after a check changes
//	TOSKAR_QUALITY_BASELINE=runs.jsonl
//	                                compares with a run saved before, its
//	                                answers checked as this run's are; the
//	                                set fails only on a significant drop

// qualityRun is one run of every case: its driver, with its seed.
type qualityRun struct {
	driver Driver
	repeat int
	// repeats is how many runs the set has, for naming.
	repeats int
}

// name is the case's subtest: its id, and its run when there's more than one.
func (r qualityRun) name(id string) string {
	if r.repeats < 2 {
		return id
	}
	return fmt.Sprintf("%s#%d", id, r.repeat+1)
}

// runPlan is the set's runs and where they're saved.
type runPlan struct {
	runs     []qualityRun
	driver   string
	sampling []sampling
	savePath string
}

func qualityRuns(t *testing.T, d Driver) runPlan {
	if path := config.Env("QUALITY_RESCORE"); path != "" {
		return rescorePlan(t, path)
	}
	repeats := 1
	if n, err := strconv.Atoi(config.Env("QUALITY_REPEATS")); err == nil && n > 1 {
		repeats = n
	}
	temperature := 0.0
	if repeats > 1 {
		temperature = 0.7
	}
	if v, err := strconv.ParseFloat(config.Env("QUALITY_TEMPERATURE"), 64); err == nil && v >= 0 {
		temperature = v
	}
	plan := runPlan{driver: d.Name(), savePath: config.Env("QUALITY_SAVE")}
	for r := range repeats {
		s := sampling{Seed: r + 1, Temperature: temperature}
		run := qualityRun{driver: d, repeat: r, repeats: repeats}
		if sd, ok := d.(interface{ withSampling(sampling) Driver }); ok {
			run.driver = sd.withSampling(s)
		}
		plan.runs = append(plan.runs, run)
		plan.sampling = append(plan.sampling, s)
	}
	return plan
}

// savedRun is one line of a saved run file.
type savedRun struct {
	Case        string  `json:"case"`
	Repeat      int     `json:"repeat"`
	Seed        int     `json:"seed"`
	Temperature float64 `json:"temperature"`
	Driver      string  `json:"driver"`
	Result      Result  `json:"result"`
}

// save writes every run's answer and what it did, one JSON object a line.
func (p runPlan) save(t *testing.T, rows []reportRow) {
	if p.savePath == "" {
		return
	}
	f, err := os.Create(p.savePath)
	if err != nil {
		t.Errorf("saving the runs: %v", err)
		return
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	for _, row := range rows {
		s := sampling{}
		if row.Repeat < len(p.sampling) {
			s = p.sampling[row.Repeat]
		}
		if err := enc.Encode(savedRun{Case: row.Case.ID, Repeat: row.Repeat, Seed: s.Seed, Temperature: s.Temperature, Driver: p.driver, Result: row.Result}); err != nil {
			t.Errorf("saving %s: %v", row.Case.ID, err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Errorf("saving the runs: %v", err)
	}
}

// savedDriver answers each case with a saved run, so the checks run again
// without a model.
type savedDriver struct {
	name    string
	repeat  int
	results map[string]map[int]Result
}

func (d savedDriver) Name() string { return d.name }

func (d savedDriver) Run(t *testing.T, c Case) Result {
	r, ok := d.results[c.ID][d.repeat]
	if !ok {
		t.Skip("not in the saved runs")
	}
	return r
}

func rescorePlan(t *testing.T, path string) runPlan {
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("rescoring: %v", err)
	}
	results := map[string]map[int]Result{}
	plan := runPlan{}
	repeats := 0
	for i, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var s savedRun
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			t.Fatalf("rescoring: line %d: %v", i+1, err)
		}
		if results[s.Case] == nil {
			results[s.Case] = map[int]Result{}
		}
		results[s.Case][s.Repeat] = s.Result
		plan.driver = s.Driver
		repeats = max(repeats, s.Repeat+1)
		for len(plan.sampling) <= s.Repeat {
			plan.sampling = append(plan.sampling, sampling{})
		}
		plan.sampling[s.Repeat] = sampling{Seed: s.Seed, Temperature: s.Temperature}
	}
	for r := range repeats {
		plan.runs = append(plan.runs, qualityRun{driver: savedDriver{name: plan.driver, repeat: r, results: results}, repeat: r, repeats: repeats})
	}
	return plan
}

// summary is how a set's runs went.
type summary struct {
	runs, passes int
	rate, lo, hi float64
	repeats      int
	// planned are the cases the set has for this platform; missing are the
	// ones that didn't run, such as after a run stopped.
	planned, missing []string
	// perCase is each case's passes and runs.
	perCase map[string][2]int
	// flaky are cases that passed some runs but not all.
	flaky []string
	// passAt3 and majority3 are the set's averages, from cases with three
	// or more runs; NaN with fewer.
	passAt3, majority3 float64
}

func summarizeRuns(rows []reportRow, planned []string) summary {
	s := summary{planned: planned, perCase: map[string][2]int{}, passAt3: math.NaN(), majority3: math.NaN()}
	for _, row := range rows {
		pc := s.perCase[row.Case.ID]
		pc[1]++
		s.runs++
		if len(row.Failures) == 0 {
			pc[0]++
			s.passes++
		}
		s.perCase[row.Case.ID] = pc
		s.repeats = max(s.repeats, row.Repeat+1)
	}
	s.rate = 1
	if s.runs > 0 {
		s.rate = float64(s.passes) / float64(s.runs)
	}
	s.lo, s.hi = wilson(s.passes, s.runs)
	for _, id := range planned {
		if _, ok := s.perCase[id]; !ok {
			s.missing = append(s.missing, id)
		}
	}
	ids := make([]string, 0, len(s.perCase))
	for id := range s.perCase {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var at3, maj []float64
	for _, id := range ids {
		pc := s.perCase[id]
		if pc[0] > 0 && pc[0] < pc[1] {
			s.flaky = append(s.flaky, id)
		}
		if pc[1] >= 3 {
			at3 = append(at3, passAtK(pc[1], pc[0], 3))
			maj = append(maj, majorityOf3(pc[1], pc[0]))
		}
	}
	if len(at3) > 0 {
		s.passAt3, s.majority3 = mean(at3), mean(maj)
	}
	return s
}

// summarizeRows is summarizeRuns for a set whose every case is in rows,
// such as the files and media sets.
func summarizeRows(rows []reportRow) summary {
	var ids []string
	for _, row := range rows {
		if !slices.Contains(ids, row.Case.ID) {
			ids = append(ids, row.Case.ID)
		}
	}
	return summarizeRuns(rows, ids)
}

func mean(xs []float64) float64 {
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

// complete reports a run that covered every case, the only kind to compare
// with another full run.
func (s summary) complete() bool { return len(s.missing) == 0 }

// sentence is the headline: "64 of 70 runs passed (91%, 95% interval 83–96%)".
func (s summary) sentence() string {
	what := "cases"
	if s.repeats > 1 {
		what = "runs"
	}
	return fmt.Sprintf("%d of %d %s passed (%.0f%%, 95%% interval %.0f–%.0f%%)", s.passes, s.runs, what, s.rate*100, s.lo*100, s.hi*100)
}

// line is the headline with the coverage, for the test log.
func (s summary) line() string {
	out := s.sentence()
	if !s.complete() {
		out += fmt.Sprintf("; covered %d of %d cases, so it isn't a full run", len(s.planned)-len(s.missing), len(s.planned))
	}
	return out
}

// details writes the coverage, the repeats' numbers, and the flaky cases.
func (s summary) details(b *strings.Builder) {
	if s.complete() {
		fmt.Fprintf(b, "Covered all %d cases.", len(s.planned))
	} else {
		fmt.Fprintf(b, "**Covered %d of %d cases**, so this isn't a full run to compare with others. Not run: %s.", len(s.planned)-len(s.missing), len(s.planned), strings.Join(s.missing, ", "))
	}
	if s.repeats > 1 {
		fmt.Fprintf(b, " Each case ran %d times.", s.repeats)
	}
	b.WriteString("\n\n")
	if !math.IsNaN(s.passAt3) {
		fmt.Fprintf(b, "From the repeats: any of three tries passes %.0f%% of the time (pass@3), and the answer two of three agree on, if agreeing meant passing, %.0f%% (majority of 3).\n\n", s.passAt3*100, s.majority3*100)
	}
	if len(s.flaky) > 0 {
		b.WriteString("Flaky, passing some runs but not all:\n\n")
		for _, id := range s.flaky {
			pc := s.perCase[id]
			fmt.Fprintf(b, "- %s: %d of %d\n", id, pc[0], pc[1])
		}
		b.WriteString("\n")
	}
}

func TestSummarize(t *testing.T) {
	row := func(id string, repeat int, pass bool) reportRow {
		r := reportRow{Case: Case{ID: id}, Repeat: repeat}
		if !pass {
			r.Failures = []string{"no"}
		}
		return r
	}
	rows := []reportRow{
		row("a", 0, true), row("a", 1, true), row("a", 2, true),
		row("b", 0, true), row("b", 1, false), row("b", 2, true),
		row("c", 0, false), row("c", 1, false), row("c", 2, false),
	}
	s := summarizeRuns(rows, []string{"a", "b", "c", "d"})
	if s.passes != 5 || s.runs != 9 || s.repeats != 3 {
		t.Fatalf("passes %d of %d over %d repeats", s.passes, s.runs, s.repeats)
	}
	if !slices.Equal(s.flaky, []string{"b"}) || !slices.Equal(s.missing, []string{"d"}) || s.complete() {
		t.Errorf("flaky %v, missing %v", s.flaky, s.missing)
	}
	// a always, b two of three, c never: pass@3 (1+1+0)/3; majority (1+1+0)/3.
	if math.Abs(s.passAt3-2.0/3) > 1e-9 || math.Abs(s.majority3-2.0/3) > 1e-9 {
		t.Errorf("pass@3 %v, majority %v", s.passAt3, s.majority3)
	}
	if got := s.sentence(); !strings.HasPrefix(got, "5 of 9 runs passed (56%, 95% interval") {
		t.Errorf("sentence %q", got)
	}
	if got := s.line(); !strings.Contains(got, "covered 3 of 4 cases") {
		t.Errorf("line %q", got)
	}
	var b strings.Builder
	s.details(&b)
	for _, want := range []string{"Not run: d", "Each case ran 3 times", "b: 2 of 3", "pass@3"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("details lack %q:\n%s", want, b.String())
		}
	}
}

// A saved run checks the same again, so a check that changes can be tried
// on answers already written.
func TestSaveAndRescore(t *testing.T) {
	path := t.TempDir() + "/runs.jsonl"
	plan := runPlan{driver: "served", savePath: path, sampling: []sampling{{1, 0.7}, {2, 0.7}}}
	plan.save(t, []reportRow{
		{Case: Case{ID: "a"}, Repeat: 0, Result: Result{Answer: "Sitka"}},
		{Case: Case{ID: "a"}, Repeat: 1, Result: Result{Failed: "turn failed: no answer"}},
	})
	t.Setenv("TOSKAR_QUALITY_RESCORE", path)
	again := qualityRuns(t, stubDriver{})
	if len(again.runs) != 2 || again.driver != "served" || again.sampling[1].Seed != 2 {
		t.Fatalf("rescore plan %+v", again)
	}
	c := Case{ID: "a", What: "where", Expect: Expect{AnswerMatches: "(?i)sitka"}}
	if r := again.runs[0].driver.Run(t, c); r.Answer != "Sitka" || len(check("served", c, r, nil)) != 0 {
		t.Errorf("the first run %+v", r)
	}
	if f := check("served", c, again.runs[1].driver.Run(t, c), nil); len(f) != 1 || !strings.Contains(f[0], "turn failed") {
		t.Errorf("a failed turn checks as %v", f)
	}
}

// A real model that writes no answer fails its case, and the run goes on
// (#834): the 1.11.2 release stopped at one such case, at 64 of 70.
func TestEmptyAnswerIsAFailedCase(t *testing.T) {
	var seeds []float64
	var mu sync.Mutex
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		if s, ok := req["seed"].(float64); ok {
			seeds = append(seeds, s)
		}
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": ""}}}})
	}))
	defer model.Close()
	d := stubDriver{model: model.URL}.withSampling(sampling{Seed: 2, Temperature: 0.7})
	c := Case{ID: "empty", What: "a plain question", Message: "What is the capital of Alaska?"}
	r := d.Run(t, c)
	if !strings.Contains(r.Failed, "failed") {
		t.Fatalf("an empty answer is %+v, want a failed turn", r)
	}
	if f := check(d.Name(), c, r, nil); len(f) != 1 {
		t.Errorf("checks %v, want the one failure", f)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seeds) == 0 || seeds[0] != 2 {
		t.Errorf("the model got seeds %v, want the run's seed 2", seeds)
	}
}

// baselineSummary is the saved run at TOSKAR_QUALITY_BASELINE, its answers
// checked with today's checks, so a changed check isn't a regression; nil
// without one. Only the cases this run planned count, so the two compare.
func baselineSummary(t *testing.T, cases []Case, planned []string, deflection *regexp.Regexp) *summary {
	path := config.Env("QUALITY_BASELINE")
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("the baseline: %v", err)
		return nil
	}
	byID := map[string]Case{}
	for _, c := range cases {
		byID[c.ID] = c
	}
	var rows []reportRow
	for i, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var s savedRun
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			t.Errorf("the baseline: line %d: %v", i+1, err)
			return nil
		}
		c, ok := byID[s.Case]
		if !ok || !slices.Contains(planned, s.Case) {
			continue
		}
		rows = append(rows, reportRow{Case: c, Failures: check(s.Driver, c, s.Result, deflection), Repeat: s.Repeat, Result: s.Result})
	}
	sum := summarizeRuns(rows, planned)
	return &sum
}
