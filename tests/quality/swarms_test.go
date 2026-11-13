package quality

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/config"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// The swarm set (#1135, docs/features/swarms.md): multi-part questions and
// long tasks in a folder, each asked of a single agent and of a swarm on the
// same models, to measure whether a swarm helps: tasks done, time, and
// tokens. It's the comparison Deliberate's was (#459): a feature on and off,
// with nothing else changed.
//
// Each item runs in arms: single, once per model; split, on all the models
// (its agents take them in turn); and race, when there are two or more.
// TOSKAR_QUALITY_SWARM_MODELS lists the models, comma-separated (the run's
// model when empty), and TOSKAR_QUALITY_SWARM_SLOTS sets each one's slots
// first, so one model on one computer can run two agents at once.
//
// A task's agents work in copies of a Git folder, and their change is applied
// to it once the person allows it, so this set answers yes to writing files in
// the item's folder or its agents' copies, and to applying the swarm's change
// there, in both arms (Setup.AllowWrites). Every other approval is still
// answered no. A task is done when the folder's tests pass, with the test
// files as they were.
//
// The stub runs a question's and a task's scripts in both arms with two
// slots, which checks the plumbing and the scoring; a real model's numbers are
// measured, not held to a bar. TOSKAR_QUALITY_SWARMS_REPORT writes a Markdown report and
// TOSKAR_QUALITY_SWARMS_JSON the results.

// swarmItem is one item of the set.
type swarmItem struct {
	ID, What, Message string
	// Facts are what a question's answer must hold, one pattern for each
	// fact, from the set's fixed web pages.
	Facts []string
	// Project is a task's folder; Tests are its test files, which must stay
	// as they are.
	Project map[string]string
	Tests   []string
	// Single is the stub's script for one agent, and Split its rules for a
	// swarm: the planner's parts, each agent's work, and the answer.
	Single []string
	Split  []StubRule
}

func (it swarmItem) task() bool { return len(it.Project) > 0 }

// plannerAsks is in the planner's instructions, for a question or a task.
const plannerAsks = "You coordinate several assistants"

// partsJSON is the planner's reply with these parts: title, then paths.
func partsJSON(parts ...[]string) string {
	type part struct {
		Title        string   `json:"title"`
		Instructions string   `json:"instructions"`
		Paths        []string `json:"paths,omitempty"`
	}
	var out struct {
		Parts []part `json:"parts"`
	}
	for _, p := range parts {
		out.Parts = append(out.Parts, part{Title: p[0], Instructions: p[1], Paths: p[2:]})
	}
	raw, _ := json.Marshal(out)
	return string(raw)
}

// agentRule is agent n's replies.
func agentRule(n int, replies ...string) StubRule {
	return StubRule{Match: fmt.Sprintf("You are agent %d of", n), Replies: replies}
}

// writeFile is a scripted write of a whole file.
func writeFile(path, content string) string {
	return toolCall("filesystem.write", map[string]any{"path": path, "content": content})
}

// question is a multi-part question item, its stub answering each part.
func question(id, what, message string, facts []string, parts [][2]string) swarmItem {
	var answers []string
	var titled [][]string
	for _, p := range parts {
		answers = append(answers, p[1])
		titled = append(titled, []string{p[0], p[0]})
	}
	whole := strings.Join(answers, " ")
	rules := []StubRule{{Match: plannerAsks, Replies: []string{partsJSON(titled...)}}}
	for i, a := range answers {
		rules = append(rules, agentRule(i+1, a))
	}
	rules = append(rules, StubRule{Replies: []string{whole}})
	return swarmItem{ID: id, What: what, Message: message, Facts: facts,
		Single: []string{whole, whole, whole}, Split: rules}
}

// fix is one part of a task: the file to change, what it becomes, and the
// planner's title for it.
type fix struct{ title, path, content string }

// task is a task item. Its stub writes every fix as one agent, and each fix
// as its own agent in a swarm.
func task(id, what, message string, project map[string]string, tests []string, fixes ...fix) swarmItem {
	var single []string
	var parts [][]string
	rules := []StubRule{{Match: plannerAsks}}
	for i, f := range fixes {
		single = append(single, writeFile(f.path, f.content))
		parts = append(parts, []string{f.title, "Fix the failing test for " + f.title + ", changing only " + f.path + ".", f.path})
		rules = append(rules, agentRule(i+1, writeFile(f.path, f.content), "I changed "+f.path+", and its tests pass."))
	}
	rules[0].Replies = []string{partsJSON(parts...)}
	done := "I changed the code so go test ./... passes, without touching the tests."
	single = append(single, done)
	rules = append(rules, StubRule{Replies: []string{done}})
	return swarmItem{ID: id, What: what, Message: message, Project: project, Tests: tests, Single: single, Split: rules}
}

// The tasks' projects: Go modules whose parts are in packages of their own,
// so they can be done separately.

var shopProject = map[string]string{
	"go.mod":              "module example.com/shop\n\ngo 1.22\n",
	"price/price.go":      "package price\n\n// WithTax adds tax at ratePercent to a price in cents.\nfunc WithTax(cents, ratePercent int) int {\n\treturn cents + cents*ratePercent/10\n}\n",
	"price/price_test.go": "package price\n\nimport \"testing\"\n\nfunc TestWithTax(t *testing.T) {\n\tif got := WithTax(1000, 8); got != 1080 {\n\t\tt.Fatalf(\"WithTax(1000, 8) = %d, want 1080\", got)\n\t}\n}\n",
	"names/names.go":      "package names\n\nimport \"strings\"\n\n// Initials are the first letter of each word, as capitals.\nfunc Initials(full string) string {\n\tvar b strings.Builder\n\tfor _, w := range strings.Fields(full) {\n\t\tb.WriteString(w[:1])\n\t}\n\treturn b.String()\n}\n",
	"names/names_test.go": "package names\n\nimport \"testing\"\n\nfunc TestInitials(t *testing.T) {\n\tif got := Initials(\"ada lovelace\"); got != \"AL\" {\n\t\tt.Fatalf(\"Initials = %q, want AL\", got)\n\t}\n}\n",
	"README.md":           "# Shop\n\nPrices and names. `go test ./...` runs the tests.\n",
}

var kitProject = map[string]string{
	"go.mod":            "module example.com/kit\n\ngo 1.22\n",
	"strs/strs.go":      "// Package strs has helpers for strings.\npackage strs\n",
	"strs/strs_test.go": "package strs\n\nimport \"testing\"\n\nfunc TestReverse(t *testing.T) {\n\tfor in, want := range map[string]string{\"abc\": \"cba\", \"héllo\": \"olléh\", \"\": \"\"} {\n\t\tif got := Reverse(in); got != want {\n\t\t\tt.Errorf(\"Reverse(%q) = %q, want %q\", in, got, want)\n\t\t}\n\t}\n}\n",
	"nums/nums.go":      "// Package nums has helpers for numbers.\npackage nums\n",
	"nums/nums_test.go": "package nums\n\nimport \"testing\"\n\nfunc TestClamp(t *testing.T) {\n\tcases := [][4]int{{5, 0, 10, 5}, {-3, 0, 10, 0}, {42, 0, 10, 10}}\n\tfor _, c := range cases {\n\t\tif got := Clamp(c[0], c[1], c[2]); got != c[3] {\n\t\t\tt.Errorf(\"Clamp(%d, %d, %d) = %d, want %d\", c[0], c[1], c[2], got, c[3])\n\t\t}\n\t}\n}\n",
}

var unitsProject = map[string]string{
	"go.mod":                "module example.com/units\n\ngo 1.22\n",
	"temp/temp.go":          "package temp\n\n// CToF converts degrees Celsius to Fahrenheit.\nfunc CToF(c float64) float64 {\n\treturn c*5/9 + 32\n}\n",
	"temp/temp_test.go":     "package temp\n\nimport \"testing\"\n\nfunc TestCToF(t *testing.T) {\n\tfor c, want := range map[float64]float64{0: 32, 100: 212, -40: -40} {\n\t\tif got := CToF(c); got != want {\n\t\t\tt.Errorf(\"CToF(%v) = %v, want %v\", c, got, want)\n\t\t}\n\t}\n}\n",
	"length/length.go":      "// Package length converts lengths.\npackage length\n\n// KmToMiles converts kilometers to miles.\nfunc KmToMiles(km float64) float64 { return km / 1.609344 }\n",
	"length/length_test.go": "package length\n\nimport (\n\t\"math\"\n\t\"testing\"\n)\n\nfunc TestMilesToKm(t *testing.T) {\n\tif got := MilesToKm(10); math.Abs(got-16.09344) > 1e-9 {\n\t\tt.Fatalf(\"MilesToKm(10) = %v, want 16.09344\", got)\n\t}\n}\n",
}

var blogProject = map[string]string{
	"go.mod":              "module example.com/blog\n\ngo 1.22\n",
	"slug/slug.go":        "package slug\n\nimport \"strings\"\n\n// Slug makes a title into a lower-case path, words joined by hyphens.\nfunc Slug(title string) string {\n\treturn strings.Join(strings.Fields(title), \"-\")\n}\n",
	"slug/slug_test.go":   "package slug\n\nimport \"testing\"\n\nfunc TestSlug(t *testing.T) {\n\tif got := Slug(\"Hello  World\"); got != \"hello-world\" {\n\t\tt.Fatalf(\"Slug = %q, want hello-world\", got)\n\t}\n}\n",
	"words/words.go":      "package words\n\nimport \"strings\"\n\n// Count counts the words in s.\nfunc Count(s string) int {\n\treturn len(strings.Split(s, \" \"))\n}\n",
	"words/words_test.go": "package words\n\nimport \"testing\"\n\nfunc TestCount(t *testing.T) {\n\tfor s, want := range map[string]int{\"one two  three\": 3, \"\": 0, \" lead\": 1} {\n\t\tif got := Count(s); got != want {\n\t\t\tt.Errorf(\"Count(%q) = %d, want %d\", s, got, want)\n\t\t}\n\t}\n}\n",
	"stats/stats.go":      "package stats\n\n// Mean is the average of xs, or 0 for none.\nfunc Mean(xs []int) float64 {\n\tif len(xs) == 0 {\n\t\treturn 0\n\t}\n\tsum := 0\n\tfor _, x := range xs {\n\t\tsum += x\n\t}\n\treturn float64(sum / len(xs))\n}\n",
	"stats/stats_test.go": "package stats\n\nimport \"testing\"\n\nfunc TestMean(t *testing.T) {\n\tif got := Mean([]int{1, 2}); got != 1.5 {\n\t\tt.Fatalf(\"Mean = %v, want 1.5\", got)\n\t}\n}\n",
}

func swarmItems() []swarmItem {
	return []swarmItem{
		question("marathon_and_rate", "A race result and an exchange rate",
			"Two things: who won the women's race at the 2026 Juneau Marathon, and in what time? And what's the US dollar to euro exchange rate right now?",
			[]string{`Ana Ruiz`, `2:51:07`, `0\.92`},
			[][2]string{{"Marathon winner", "Ana Ruiz won the women's race at the 2026 Juneau Marathon in 2:51:07."}, {"Exchange rate", "1 US dollar is 0.92 euros."}}),
		question("coffee_and_forecast", "A coffee shop's hours and the forecast",
			"I'm going downtown in Juneau tomorrow morning. Where is the Heritage coffee shop downtown and what time does it open? And what's the weather forecast for Juneau?",
			[]string{`130 Front`, `\b6(:00)?\s*(am|a\.m\.)`, `\b48\b`},
			[][2]string{{"Coffee shop", "Heritage Coffee is at 130 Front St and opens at 6 am."}, {"Forecast", "The forecast is rain likely, with a high near 48."}}),
		question("bitcoin_and_go", "A price and a software release",
			"What's the price of bitcoin today, and what's the latest release of the Go programming language?",
			[]string{`64,?210`, `1\.26\.3`},
			[][2]string{{"Bitcoin price", "Bitcoin is trading at $64,210."}, {"Go release", "The latest Go release is go1.26.3."}}),
		question("kayak_and_marathon", "A shop's prices and a race result",
			"Where can I buy a used kayak in Juneau, and from what price? And who won the men's race at the 2026 Juneau Marathon?",
			[]string{`Juneau Outdoor Center`, `\$?350\b`, `Tom Becker`},
			[][2]string{{"Used kayaks", "Juneau Outdoor Center sells used kayaks from $350."}, {"Men's winner", "Tom Becker won the men's race in 2:38:44."}}),
		question("espresso_and_library", "A product pick and a local news item",
			"What's a good espresso machine under $200, and what does it cost? And what's in the Juneau news about the library's hours?",
			[]string{`Stilosa`, `\$?119\b`, `\b8\s*(pm|p\.m\.)`},
			[][2]string{{"Espresso machine", "The De'Longhi Stilosa, at $119."}, {"Library hours", "The downtown library will stay open until 8 pm on weekdays."}}),
		question("june_weather_and_bikes", "Typical weather and a product's limit",
			"What's the weather usually like in Juneau in June, with the average high? And which bikes are rated for riders up to 550 lbs?",
			[]string{`\b61\b`, `Zize`},
			[][2]string{{"June weather", "June in Juneau averages a high of 61°F, often cloudy with light rain."}, {"Bikes", "Zize bikes are rated for riders up to 550 lbs."}}),
		task("two_failing_packages", "Fix two packages' failing tests",
			"Both packages in this folder have a failing test. Fix the code so go test ./... passes, without changing the tests.",
			shopProject, []string{"price/price_test.go", "names/names_test.go"},
			fix{"tax", "price/price.go", strings.Replace(shopProject["price/price.go"], "/10\n", "/100\n", 1)},
			fix{"initials", "names/names.go", strings.Replace(shopProject["names/names.go"], "w[:1]", "strings.ToUpper(w[:1])", 1)}),
		task("add_two_functions", "Add two functions the tests expect",
			"The tests in this folder call two functions that don't exist yet: strs.Reverse and nums.Clamp. Add them so go test ./... passes, without changing the tests.",
			kitProject, []string{"strs/strs_test.go", "nums/nums_test.go"},
			fix{"Reverse", "strs/strs.go", kitProject["strs/strs.go"] + "\n// Reverse reverses s by runes.\nfunc Reverse(s string) string {\n\tr := []rune(s)\n\tfor i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {\n\t\tr[i], r[j] = r[j], r[i]\n\t}\n\treturn string(r)\n}\n"},
			fix{"Clamp", "nums/nums.go", kitProject["nums/nums.go"] + "\n// Clamp keeps v between lo and hi.\nfunc Clamp(v, lo, hi int) int {\n\treturn min(max(v, lo), hi)\n}\n"}),
		task("bug_and_feature", "Fix a bug in one package and add a function to another",
			"Fix the temperature conversion so its test passes, and add length.MilesToKm (1 mile is 1.609344 km) so its test passes. Don't change the tests.",
			unitsProject, []string{"temp/temp_test.go", "length/length_test.go"},
			fix{"temperature", "temp/temp.go", strings.Replace(unitsProject["temp/temp.go"], "c*5/9 + 32", "c*9/5 + 32", 1)},
			fix{"miles", "length/length.go", unitsProject["length/length.go"] + "\n// MilesToKm converts miles to kilometers.\nfunc MilesToKm(miles float64) float64 { return miles * 1.609344 }\n"}),
		task("three_packages", "Fix three packages' failing tests",
			"Three packages in this folder have failing tests. Fix the code so go test ./... passes, without changing the tests.",
			blogProject, []string{"slug/slug_test.go", "words/words_test.go", "stats/stats_test.go"},
			fix{"slug", "slug/slug.go", strings.Replace(blogProject["slug/slug.go"], "strings.Join(strings.Fields(title), \"-\")", "strings.ToLower(strings.Join(strings.Fields(title), \"-\"))", 1)},
			fix{"words", "words/words.go", strings.Replace(blogProject["words/words.go"], "strings.Split(s, \" \")", "strings.Fields(s)", 1)},
			fix{"mean", "stats/stats.go", strings.Replace(blogProject["stats/stats.go"], "float64(sum / len(xs))", "float64(sum) / float64(len(xs))", 1)}),
	}
}

// swarmArm is one way an item is asked: by one agent on one model, or by a
// swarm on all of them.
type swarmArm struct {
	// Name is single, split, or race.
	Name   string   `json:"arm"`
	Models []string `json:"models"`
}

// swarmArms are the arms for these models: single on each, split on all,
// and race on all when there are two or more.
func swarmArms(models []string) []swarmArm {
	var arms []swarmArm
	for _, m := range models {
		arms = append(arms, swarmArm{Name: "single", Models: []string{m}})
	}
	arms = append(arms, swarmArm{Name: "split", Models: models})
	if len(models) >= 2 {
		arms = append(arms, swarmArm{Name: "race", Models: models})
	}
	return arms
}

// label names an arm's models: "auto" for the daemon's own pick.
func (a swarmArm) label() string {
	names := make([]string, len(a.Models))
	for i, m := range a.Models {
		names[i] = m
		if m == "" {
			names[i] = "auto"
		}
	}
	return strings.Join(names, " + ")
}

// swarmResult is one item in one arm.
type swarmResult struct {
	Item   string   `json:"item"`
	Kind   string   `json:"kind"`
	Arm    string   `json:"arm"`
	Models []string `json:"models"`
	// Ran says it was measured: a turn that stopped before it could be
	// checked wasn't, and none of its figures are.
	Ran  bool   `json:"ran"`
	Done bool   `json:"done"`
	Why  string `json:"why,omitempty"`
	// Facts is how many of a question's facts the answer has.
	Facts   int     `json:"facts,omitempty"`
	Seconds float64 `json:"seconds"`
	// Tokens are the turn's and its agents': prompt and completion. -1
	// when the daemon didn't say.
	Tokens int `json:"tokens"`
	// AtOnce is the most agents whose turns overlapped: 1 for one agent.
	AtOnce    int `json:"at_once"`
	Agents    int `json:"agents"`
	Computers int `json:"computers"`
	// Single is the code Toskar gave for running a swarm as one agent, or
	// differently than asked, and Note its line.
	Single string `json:"single,omitempty"`
	Note   string `json:"note,omitempty"`
	// Applied and Checks are a task swarm's: the change was applied to the
	// folder, and how its checks went on the merge.
	Applied bool   `json:"applied,omitempty"`
	Checks  string `json:"checks,omitempty"`
}

// scoreQuestion counts the facts an answer holds; it's done with all of them.
func scoreQuestion(it swarmItem, answer string) (int, string) {
	got := 0
	var missing []string
	for _, f := range it.Facts {
		if regexp.MustCompile(`(?i)` + f).MatchString(answer) {
			got++
		} else {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		return got, "the answer lacks " + strings.Join(missing, ", ")
	}
	return got, ""
}

// scoreTask says whether a task is done in its folder: the tests are as they
// were, and pass.
func scoreTask(it swarmItem, dir string) (bool, string) {
	for _, name := range it.Tests {
		if !unchanged(dir, name, it.Project[name]) {
			return false, "it changed " + name
		}
	}
	return goTestPasses(dir)
}

// swarmOf is the id of the swarm a turn started, from its events.
func swarmOf(events []Event) string {
	for _, e := range events {
		if id, _ := e.Payload["swarm_id"].(string); id != "" {
			return id
		}
	}
	return ""
}

// atOnce is the most agents running at the same time, from the swarm's
// events in the order they came: 1 when no agent started, since the turn
// itself was the one agent.
func atOnce(events []Event) int {
	running, most := 0, 1
	for _, e := range events {
		switch e.Type {
		case "swarm.agent.started":
			running++
			most = max(most, running)
		case "swarm.agent.finished":
			running = max(0, running-1)
		}
	}
	return most
}

// measureSwarm fills in what a turn cost and how its swarm ran.
func measureSwarm(res *swarmResult, r Result) {
	res.Tokens, res.AtOnce, res.Agents, res.Computers = -1, atOnce(r.Events), 1, 1
	if r.Run != nil {
		res.Tokens = 0
		for _, m := range r.Run.Models {
			res.Tokens += m.PromptTokens + m.CompletionTokens
		}
		if len(r.Run.Nodes) > 0 {
			res.Computers = len(r.Run.Nodes)
		}
	}
	for _, e := range r.Events {
		if e.Type == "swarm.note" && res.Single == "" {
			res.Single, _ = e.Payload["reason"].(string)
			res.Note, _ = e.Payload["note"].(string)
		}
	}
	s := r.Swarm
	if s == nil {
		return
	}
	// The agents' turns are chats of their own, so their tokens are the
	// swarm's, beside the coordinator's.
	if res.Tokens >= 0 {
		res.Tokens += s.Tokens
	}
	if len(s.Agents) > 0 {
		res.Agents = len(s.Agents)
		nodes := map[string]bool{}
		for _, a := range s.Agents {
			nodes[a.NodeID] = true
		}
		res.Computers = len(nodes)
	}
	res.Applied, res.Checks = s.Applied, s.Checks
	if res.Note == "" {
		res.Note = s.Note
	}
}

// swarmRow is a model or models in one arm, over every item.
type swarmRow struct {
	Arm    string   `json:"arm"`
	Models []string `json:"models"`
	// Runs is how each model ran, from the runtime's report: full,
	// partial, or cpu; absent when it didn't say.
	Runs map[string]string `json:"runs,omitempty"`
	// Complete says every item was measured. Only complete rows have
	// totals, and only they are compared.
	Complete bool `json:"complete"`
	Measured int  `json:"measured"`
	Items    int  `json:"items"`
	Done     int  `json:"done,omitempty"`
	// TasksDone and QuestionsDone split Done by kind.
	TasksDone     int     `json:"tasks_done,omitempty"`
	QuestionsDone int     `json:"questions_done,omitempty"`
	Seconds       float64 `json:"seconds,omitempty"`
	// Tokens is 0 when any item's tokens weren't measured.
	Tokens int `json:"tokens,omitempty"`
	AtOnce int `json:"most_at_once,omitempty"`
	// OneAgent counts the items its swarm ran as one agent, by reason.
	OneAgent map[string]int `json:"one_agent,omitempty"`
}

// swarmRows totals each arm's results. A row is complete when every item was
// measured; an incomplete row has no totals, since they'd hold only some of
// the items.
func swarmRows(arms []swarmArm, items int, results []swarmResult) []swarmRow {
	var rows []swarmRow
	for _, arm := range arms {
		row := swarmRow{Arm: arm.Name, Models: arm.Models, Items: items}
		var mine []swarmResult
		for _, r := range results {
			if r.Arm == arm.Name && slices.Equal(r.Models, arm.Models) && r.Ran {
				mine = append(mine, r)
			}
		}
		row.Measured = len(mine)
		row.Complete = items > 0 && row.Measured == items
		if !row.Complete {
			rows = append(rows, row)
			continue
		}
		tokensKnown := true
		for _, r := range mine {
			if r.Done {
				row.Done++
				if r.Kind == "task" {
					row.TasksDone++
				} else {
					row.QuestionsDone++
				}
			}
			row.Seconds += r.Seconds
			if r.Tokens < 0 {
				tokensKnown = false
			}
			row.Tokens += r.Tokens
			row.AtOnce = max(row.AtOnce, r.AtOnce)
			if r.Single != "" {
				if row.OneAgent == nil {
					row.OneAgent = map[string]int{}
				}
				row.OneAgent[r.Single]++
			}
		}
		row.Seconds = math.Round(row.Seconds*10) / 10
		if !tokensKnown {
			row.Tokens = 0
		}
		rows = append(rows, row)
	}
	return rows
}

// swarmDiff is a swarm row against a single agent's on one of its models:
// swarm minus single.
type swarmDiff struct {
	Swarm   string  `json:"swarm"`
	Models  string  `json:"models"`
	Against string  `json:"against"`
	Done    int     `json:"done"`
	Seconds float64 `json:"seconds"`
	// Tokens is absent when either row's tokens weren't measured.
	Tokens *int `json:"tokens,omitempty"`
	// SecondsShare and TokensShare are the change as a share of single's.
	SecondsShare float64  `json:"seconds_share"`
	TokensShare  *float64 `json:"tokens_share,omitempty"`
}

// swarmSummary compares every complete swarm row with every complete single
// row on one of its models. Incomplete rows aren't compared.
func swarmSummary(rows []swarmRow) []swarmDiff {
	var out []swarmDiff
	for _, s := range rows {
		if s.Arm == "single" || !s.Complete {
			continue
		}
		for _, one := range rows {
			if one.Arm != "single" || !one.Complete || !slices.Contains(s.Models, one.Models[0]) {
				continue
			}
			d := swarmDiff{Swarm: s.Arm, Models: swarmArm{Models: s.Models}.label(), Against: swarmArm{Models: one.Models}.label(),
				Done: s.Done - one.Done, Seconds: math.Round((s.Seconds-one.Seconds)*10) / 10}
			if one.Seconds > 0 {
				d.SecondsShare = math.Round((s.Seconds-one.Seconds)/one.Seconds*1000) / 1000
			}
			if s.Tokens > 0 && one.Tokens > 0 {
				diff := s.Tokens - one.Tokens
				share := math.Round(float64(diff)/float64(one.Tokens)*1000) / 1000
				d.Tokens, d.TokensShare = &diff, &share
			}
			out = append(out, d)
		}
	}
	return out
}

// swarmModels are the models the set compares: TOSKAR_QUALITY_SWARM_MODELS,
// comma-separated, or the run's own model.
func swarmModels(d Driver) []string {
	if list := config.Env("QUALITY_SWARM_MODELS"); list != "" {
		var models []string
		for _, m := range strings.Split(list, ",") {
			if m = strings.TrimSpace(m); m != "" && !slices.Contains(models, m) {
				models = append(models, m)
			}
		}
		if len(models) > 0 {
			return models
		}
	}
	if real, ok := d.(realDriver); ok {
		return []string{real.model}
	}
	return []string{""}
}

// gitInit makes dir a Git repository with its files committed.
func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "-A"},
		{"-c", "user.name=Toskar quality", "-c", "user.email=quality@toskar.invalid", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "The task as given"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", args[0], err, out)
		}
	}
}

// TestSwarmCases checks the set itself: each task's tests fail as given and
// pass with its stub's fixes, each fix is a file of its own, and each
// question's stub answer has its facts.
func TestSwarmCases(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("the tasks' projects are Go, and go isn't installed here")
	}
	seen := map[string]bool{}
	tasks, questions := 0, 0
	for _, it := range swarmItems() {
		if seen[it.ID] {
			t.Errorf("%s: duplicate id", it.ID)
		}
		seen[it.ID] = true
		if !it.task() {
			questions++
			if len(it.Facts) < 2 {
				t.Errorf("%s: a multi-part question needs a fact for each part", it.ID)
			}
			if got, why := scoreQuestion(it, it.Single[0]); why != "" {
				t.Errorf("%s: its stub answer has %d of %d facts: %s", it.ID, got, len(it.Facts), why)
			}
			continue
		}
		tasks++
		dir := projectFolder(t, it.Project)
		if ok, _ := scoreTask(it, dir); ok {
			t.Errorf("%s: its tests already pass", it.ID)
		}
		paths := map[string]bool{}
		for _, reply := range it.Single {
			var call struct {
				ToolCall struct {
					Args struct {
						Path, Content string
					} `json:"args"`
				} `json:"tool_call"`
			}
			if json.Unmarshal([]byte(reply), &call) != nil || call.ToolCall.Args.Path == "" {
				continue
			}
			p := call.ToolCall.Args.Path
			if paths[p] || slices.Contains(it.Tests, p) {
				t.Errorf("%s: a fix writes %s, which another fix or a test has", it.ID, p)
			}
			paths[p] = true
			if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(p)), []byte(call.ToolCall.Args.Content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if len(paths) < 2 {
			t.Errorf("%s: a task needs two or more parts in files of their own", it.ID)
		}
		if ok, why := scoreTask(it, dir); !ok {
			t.Errorf("%s: its fixes don't pass: %s", it.ID, why)
		}
	}
	if tasks < 4 || questions < 6 {
		t.Errorf("%d tasks and %d questions; the set has at least 4 and 6", tasks, questions)
	}
}

// TestSwarmScoring checks how the set counts, from a small fixture: agents at
// once from events, facts, and rows, where an incomplete row has no totals and
// isn't compared.
func TestSwarmScoring(t *testing.T) {
	ev := func(types ...string) []Event {
		var out []Event
		for _, ty := range types {
			out = append(out, Event{Type: ty, Payload: map[string]any{"swarm_id": "s1"}})
		}
		return out
	}
	for _, tc := range []struct {
		events []Event
		want   int
	}{
		{nil, 1},
		{ev("swarm.started", "swarm.agent.started", "swarm.agent.started", "swarm.agent.finished", "swarm.agent.finished"), 2},
		// Queued: the second started once the first finished.
		{ev("swarm.agent.started", "swarm.agent.finished", "swarm.agent.started", "swarm.agent.finished"), 1},
	} {
		if got := atOnce(tc.events); got != tc.want {
			t.Errorf("atOnce = %d, want %d", got, tc.want)
		}
	}
	it := swarmItem{Facts: []string{`Ana Ruiz`, `0\.92`}}
	if got, why := scoreQuestion(it, "Ana Ruiz won; 1 USD is 0.92 EUR."); got != 2 || why != "" {
		t.Errorf("both facts: %d, %q", got, why)
	}
	if got, _ := scoreQuestion(it, "Ana Ruiz won."); got != 1 {
		t.Errorf("one fact: %d", got)
	}

	var r swarmResult
	measureSwarm(&r, Result{
		Events: append(ev("swarm.agent.started", "swarm.agent.started"), Event{Type: "swarm.note", Payload: map[string]any{"swarm_id": "s1", "reason": "notGit", "note": "Not a Git folder"}}),
		Swarm:  &contracts.Swarm{ID: "s1", Tokens: 900, Agents: []contracts.SwarmAgent{{NodeID: "a"}, {NodeID: "b"}}, Applied: true},
	})
	if r.Tokens != -1 || r.AtOnce != 2 || r.Agents != 2 || r.Computers != 2 || r.Single != "notGit" || !r.Applied {
		t.Errorf("measureSwarm without a run log: %+v", r)
	}

	arms := swarmArms([]string{"small", "large"})
	if len(arms) != 4 || arms[2].Name != "split" || arms[3].Name != "race" {
		t.Fatalf("arms: %+v", arms)
	}
	if one := swarmArms([]string{"small"}); len(one) != 2 {
		t.Errorf("one model has no race: %+v", one)
	}
	res := func(arm string, models []string, item string, done bool, seconds float64, tokens int) swarmResult {
		return swarmResult{Item: item, Kind: "task", Arm: arm, Models: models, Ran: true, Done: done, Seconds: seconds, Tokens: tokens, AtOnce: 1}
	}
	both := []string{"small", "large"}
	results := []swarmResult{
		res("single", []string{"small"}, "a", true, 10, 100), res("single", []string{"small"}, "b", false, 20, 200),
		res("single", []string{"large"}, "a", true, 30, 300), res("single", []string{"large"}, "b", true, 30, 300),
		res("split", both, "a", true, 8, 150), res("split", both, "b", true, 12, 250),
		// The race measured one item of two: incomplete.
		res("race", both, "a", true, 9, 400), {Item: "b", Arm: "race", Models: both},
	}
	rows := swarmRows(arms, 2, results)
	if !rows[0].Complete || rows[0].Done != 1 || rows[0].Seconds != 30 || rows[0].Tokens != 300 {
		t.Errorf("single small: %+v", rows[0])
	}
	if race := rows[3]; race.Complete || race.Measured != 1 || race.Done != 0 || race.Seconds != 0 {
		t.Errorf("an incomplete row has no totals: %+v", race)
	}
	sum := swarmSummary(rows)
	if len(sum) != 2 {
		t.Fatalf("split against each single, and no race: %+v", sum)
	}
	small := sum[0]
	if small.Against != "small" || small.Done != 1 || small.Seconds != -10 || small.Tokens == nil || *small.Tokens != 100 || small.SecondsShare != -0.333 {
		t.Errorf("split against single on small: %+v", small)
	}
	// A row whose tokens weren't all measured compares done and time only.
	results[4].Tokens = -1
	if s := swarmSummary(swarmRows(arms, 2, results)); s[0].Tokens != nil {
		t.Errorf("unmeasured tokens compared: %+v", s[0])
	}
	if md := swarmsReport("stub", map[string]int{}, swarmItems()[:1], results, rows, sum); !strings.Contains(md, "not compared") || !strings.Contains(md, "| split | small + large |") {
		t.Errorf("report:\n%s", md)
	}
}

// TestSwarmApprovals checks what the set answers yes to: writes in the item's
// folder or its swarm's copies, and its swarm's apply. Nothing else.
func TestSwarmApprovals(t *testing.T) {
	c := Case{Setup: Setup{AllowWrites: true}}
	project := "/projects/project-1"
	swarms := map[string]bool{"s1": true}
	ask := func(tool string, args map[string]any, swarmID string) map[string]any {
		p := map[string]any{"tool_id": tool, "args": args}
		if swarmID != "" {
			p["swarm_id"] = swarmID
		}
		return p
	}
	for _, tc := range []struct {
		what    string
		payload map[string]any
		want    bool
	}{
		{"a relative write", ask("filesystem.write", map[string]any{"path": "price/price.go"}, ""), true},
		{"a write in the folder", ask("filesystem.write", map[string]any{"path": project + "/a.go"}, ""), true},
		{"a write in an agent's copy", ask("filesystem.write", map[string]any{"path": "/data/swarms/s1/agent-1/a.go"}, "s1"), true},
		{"a write climbing out", ask("filesystem.write", map[string]any{"path": "../other/a.go"}, ""), false},
		{"a write elsewhere", ask("filesystem.write", map[string]any{"path": "/etc/hosts"}, ""), false},
		{"a write in another swarm's copy", ask("filesystem.write", map[string]any{"path": "/data/swarms/s2/agent-1/a.go"}, "s2"), false},
		{"another swarm's apply", ask("swarm.apply", map[string]any{"path": project}, "s2"), false},
		{"the swarm's apply", ask("swarm.apply", map[string]any{"path": project}, "s1"), true},
		{"an apply elsewhere", ask("swarm.apply", map[string]any{"path": "/home"}, "s1"), false},
		{"a command", ask("terminal", map[string]any{"command": "rm -rf ."}, ""), false},
	} {
		if got := approves(c, project, swarms, tc.payload); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.what, got, tc.want)
		}
	}
	if approves(Case{}, project, swarms, ask("filesystem.write", map[string]any{"path": "a.go"}, "")) {
		t.Error("a case that doesn't allow writes approved one")
	}
	if approves(c, "", swarms, ask("filesystem.write", map[string]any{"path": "a.go"}, "")) {
		t.Error("a case without a folder approved a write")
	}
}

// approves says whether the run answers yes to an approval: only in a case
// that allows writes (the swarm set), and then only to writing a file in its
// own folder or in an agent's copy of it for one of its swarms, and to
// applying one of its swarms' change to that folder. Everything else is no,
// as in the rest of the quality run.
func approves(c Case, project string, swarms map[string]bool, payload map[string]any) bool {
	if !c.Setup.AllowWrites || project == "" {
		return false
	}
	args, _ := payload["args"].(map[string]any)
	path, _ := args["path"].(string)
	swarmID, _ := payload["swarm_id"].(string)
	if swarmID != "" && !swarms[swarmID] {
		return false
	}
	switch payload["tool_id"] {
	case "swarm.apply":
		return swarmID != "" && path != "" && filepath.Clean(path) == filepath.Clean(project)
	case "filesystem.write":
		return writesInside(path, project, swarmID)
	}
	return false
}

// writesInside says a write's path is in the folder: relative and not
// climbing out, under the folder, or under the swarm's agents' copies
// (<data dir>/swarms/<swarm id>/agent-N).
func writesInside(path, project, swarmID string) bool {
	if path == "" {
		return false
	}
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") && !filepath.IsAbs(path) {
		clean := filepath.ToSlash(filepath.Clean(path))
		return clean != ".." && !strings.HasPrefix(clean, "../")
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	if root := filepath.ToSlash(filepath.Clean(project)); strings.HasPrefix(clean, root+"/") {
		return true
	}
	return swarmID != "" && strings.Contains(clean, "/swarms/"+swarmID+"/agent-")
}

// TestSwarmQuality runs the set: every item in every arm. The stub must do
// every item in every arm, with its swarms' agents running, not falling back
// to one; a real model's numbers are reported.
func TestSwarmQuality(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("the tasks' projects are Go, and go isn't installed here")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("a task swarm's agents work in Git, and git isn't installed here")
	}
	d, build := qualityDriver(t)
	real, isReal := d.(realDriver)
	if isReal {
		// A task swarm has 30 minutes by default; the request waits for it.
		was := requestTimeout
		requestTimeout = 45 * time.Minute
		t.Cleanup(func() { requestTimeout = was })
	}
	models := swarmModels(d)
	slots := map[string]int{}
	if isReal {
		for _, m := range models {
			if m != "" && m != real.model {
				real.installModel(t, m)
			}
		}
		if n := config.Env("QUALITY_SWARM_SLOTS"); n != "" {
			var want int
			if _, err := fmt.Sscan(n, &want); err != nil || want < 1 || want > 4 {
				t.Fatalf("TOSKAR_QUALITY_SWARM_SLOTS: %q isn't 1 to 4", n)
			}
			// The daemon's own pick, "recommended", is whichever models it
			// installed: each gets the slots, where it takes them.
			named := slices.DeleteFunc(slices.Clone(models), func(m string) bool { return m == "" })
			if len(named) < len(models) {
				var all []contracts.Model
				real.do(t, http.MethodGet, "/api/v1/models", nil, &all)
				for _, m := range all {
					if m.Installed && !slices.Contains(named, m.ID) {
						named = append(named, m.ID)
					}
				}
			}
			for _, m := range named {
				// A model the computer can't give that many slots says so
				// (MODEL_SLOTS_MEMORY, MODEL_SLOTS_CONTEXT), and keeps its own.
				if _, err := real.request(http.MethodPut, "/api/v1/models/"+m+"/slots", map[string]any{"slots": want}, nil, t.Logf); err != nil {
					t.Logf("slots for %s: %v", m, err)
					continue
				}
				slots[m] = want
				t.Cleanup(func() {
					_, _ = real.request(http.MethodPut, "/api/v1/models/"+m+"/slots", map[string]any{"slots": 1}, nil, t.Logf)
				})
			}
		}
	}
	arms := swarmArms(models)
	items := swarmItems()
	if d.Name() == "stub" {
		// The stub checks the plumbing and the scoring with a question and
		// the task with three parts, two at a time; TestSwarmCases checks
		// every item, and the whole set would add a minute to every CI run.
		items = slices.DeleteFunc(items, func(it swarmItem) bool { return it.ID != "marathon_and_rate" && it.ID != "three_packages" })
	}
	var results []swarmResult
	for _, it := range items {
		for _, arm := range arms {
			c := Case{ID: it.ID + "/" + arm.Name, What: it.What, Message: it.Message}
			if it.task() {
				c.Setup = Setup{Project: it.Project, Git: true, AllowWrites: true,
					Tools: map[string]string{"filesystem.write": "ask"}}
			}
			if arm.Name != "single" {
				c.Swarm = arm.Name
				if len(arm.Models) > 1 {
					c.Setup.SwarmModels = arm.Models
				}
			}
			runner := d
			if isReal {
				one := real
				one.model = arm.Models[0]
				runner = one
			}
			if runner.Name() == "stub" {
				if arm.Name == "single" {
					c.Stub = it.Single
				} else {
					c.StubBy = it.Split
				}
			}
			if stopped(t, d, c) {
				break
			}
			kind := "question"
			if it.task() {
				kind = "task"
			}
			res := swarmResult{Item: it.ID, Kind: kind, Arm: arm.Name, Models: arm.Models, Why: "stopped before it could be checked; see its log"}
			// An item left out by -run doesn't count: only the ones that ran do.
			entered := false
			t.Run(it.ID+"/"+arm.Name+"/"+arm.label(), func(t *testing.T) {
				entered = true
				started := time.Now()
				r := runner.Run(t, c)
				res.Seconds = math.Round(time.Since(started).Seconds()*10) / 10
				res.Ran, res.Why = true, ""
				measureSwarm(&res, r)
				// What it asked to do, and the answer, so the log shows why
				// an item wasn't done.
				for _, e := range r.Events {
					if e.Type == "tool.requested" {
						args, _ := json.Marshal(e.Payload["args"])
						t.Logf("asked: %v %v %.200s", e.Payload["agent"], e.Payload["tool_id"], args)
					}
				}
				switch {
				case r.Failed != "":
					res.Why = r.Failed
				case it.task():
					res.Done, res.Why = scoreTask(it, r.Project)
				default:
					res.Facts, res.Why = scoreQuestion(it, r.Answer)
					res.Done = res.Why == ""
				}
				t.Logf("%s: done %v in %.1f s, %d tokens, %d at once of %d agents on %d computers %s",
					arm.Name, res.Done, res.Seconds, res.Tokens, res.AtOnce, res.Agents, res.Computers, res.Note)
				if runner.Name() != "stub" {
					if !res.Done {
						t.Logf("NOT DONE: %s", res.Why)
					}
					return
				}
				// The stub checks the plumbing: every item done, and a
				// swarm's agents ran.
				if !res.Done {
					t.Errorf("%s: %s", arm.Name, res.Why)
				}
				if arm.Name != "single" && (res.Single != "" || res.Agents < 2 || r.Swarm == nil) {
					t.Errorf("the swarm didn't run its agents: %d agents, %q", res.Agents, res.Note)
				}
				if arm.Name != "single" && it.task() && !res.Applied {
					t.Error("the swarm's change wasn't applied")
				}
			})
			if entered {
				results = append(results, res)
			}
		}
	}
	measured := map[string]bool{}
	for _, r := range results {
		measured[r.Item] = true
	}
	rows := swarmRows(arms, len(measured), results)
	if isReal {
		for i := range rows {
			for _, m := range rows[i].Models {
				if runs := real.runsOf(t, m); runs != "" {
					if rows[i].Runs == nil {
						rows[i].Runs = map[string]string{}
					}
					rows[i].Runs[swarmArm{Models: []string{m}}.label()] = runs
				}
			}
		}
	}
	sum := swarmSummary(rows)
	for _, row := range rows {
		if row.Complete {
			t.Logf("swarms, %s on %s: %d of %d done, %.0f s, %d tokens", row.Arm, swarmArm{Models: row.Models}.label(), row.Done, row.Items, row.Seconds, row.Tokens)
		} else {
			t.Logf("swarms, %s on %s: incomplete, %d of %d measured", row.Arm, swarmArm{Models: row.Models}.label(), row.Measured, row.Items)
		}
	}
	if path := config.Env("QUALITY_SWARMS_REPORT"); path != "" {
		ran := slices.DeleteFunc(slices.Clone(items), func(it swarmItem) bool { return !measured[it.ID] })
		writeReport(t, d, build, path, swarmsReport(d.Name(), slots, ran, results, rows, sum))
	}
	if path := config.Env("QUALITY_SWARMS_JSON"); path != "" {
		out, _ := json.MarshalIndent(map[string]any{"driver": d.Name(), "build": build, "models": models, "slots": slots,
			"rows": rows, "summary": sum, "items": results}, "", "  ")
		if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
			t.Errorf("results: %v", err)
		}
	}
}

// runsOf is how a model ran, from its runtime's report once the set has
// loaded it: full, partial, or cpu; "" when the daemon doesn't say.
func (d realDriver) runsOf(t *testing.T, model string) string {
	t.Helper()
	var running []contracts.RunningModelView
	if _, err := d.request(http.MethodGet, "/api/v1/models/running", nil, &running, t.Logf); err != nil {
		return ""
	}
	for _, r := range running {
		if r.Mode == "" && r.Acceleration != nil && (r.ModelID == model || model == "") {
			runs, _ := runsFrom(r.Acceleration)
			return runs
		}
	}
	return ""
}

// swarmsReport is the Markdown report: the rows, swarm against single, then
// every item.
func swarmsReport(driver string, slots map[string]int, items []swarmItem, results []swarmResult, rows []swarmRow, sum []swarmDiff) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Swarms: core, %s model\n\n", driver)
	tasks := 0
	for _, it := range items {
		if it.task() {
			tasks++
		}
	}
	fmt.Fprintf(&b, "%d items: %d multi-part questions and %d tasks in a folder, each by one agent and by a swarm on the same models.", len(items), len(items)-tasks, tasks)
	if len(slots) > 0 {
		var set []string
		for m, n := range slots {
			set = append(set, fmt.Sprintf("%s %d", m, n))
		}
		slices.Sort(set)
		fmt.Fprintf(&b, " Slots: %s.", strings.Join(set, ", "))
	}
	b.WriteString(" Time includes loading a model that wasn't loaded. A row with an item that wasn't measured has no totals and isn't compared.\n\n")
	b.WriteString("| Arm | Models | Runs | Done | Tasks | Questions | Seconds | Tokens | Most at once | One agent |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, r := range rows {
		runs := "not measured"
		if len(r.Runs) > 0 {
			var list []string
			for m, v := range r.Runs {
				list = append(list, m+": "+v)
			}
			slices.Sort(list)
			runs = strings.Join(list, ", ")
		}
		label := swarmArm{Models: r.Models}.label()
		if !r.Complete {
			fmt.Fprintf(&b, "| %s | %s | %s | incomplete: %d of %d measured | | | | | | |\n", r.Arm, label, runs, r.Measured, r.Items)
			continue
		}
		tokens := "not measured"
		if r.Tokens > 0 {
			tokens = fmt.Sprintf("%d", r.Tokens)
		}
		var one []string
		for code, n := range r.OneAgent {
			one = append(one, fmt.Sprintf("%s %d", code, n))
		}
		slices.Sort(one)
		fmt.Fprintf(&b, "| %s | %s | %s | %d of %d | %d | %d | %.0f | %s | %d | %s |\n",
			r.Arm, label, runs, r.Done, r.Items, r.TasksDone, r.QuestionsDone, r.Seconds, tokens, r.AtOnce, strings.Join(one, ", "))
	}
	b.WriteString("\n## Swarm against a single agent\n\nSwarm minus single, over the whole set.\n\n")
	if len(sum) == 0 {
		b.WriteString("Nothing compared: no swarm row and single row were both complete, so these are not compared.\n")
	} else {
		b.WriteString("| Swarm | Models | Against single on | Done | Seconds | Tokens |\n| --- | --- | --- | --- | --- | --- |\n")
		for _, s := range sum {
			tokens := "not measured"
			if s.Tokens != nil {
				tokens = fmt.Sprintf("%+d (%+.0f%%)", *s.Tokens, *s.TokensShare*100)
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %+d | %+.0f (%+.0f%%) | %s |\n", s.Swarm, s.Models, s.Against, s.Done, s.Seconds, s.SecondsShare*100, tokens)
		}
	}
	for _, r := range rows {
		if !r.Complete {
			fmt.Fprintf(&b, "\n%s on %s is not compared: %d of %d items were measured.\n", r.Arm, swarmArm{Models: r.Models}.label(), r.Measured, r.Items)
		}
	}
	b.WriteString("\n## Items\n\n| Item | Kind | Arm | Models | Done | Seconds | Tokens | At once | Agents | Computers | Swarm note | Why not done |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, r := range results {
		label := swarmArm{Models: r.Models}.label()
		if !r.Ran {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | not measured | | | | | | | %s |\n", r.Item, r.Kind, r.Arm, label, strings.ReplaceAll(r.Why, "|", "/"))
			continue
		}
		mark := "no"
		if r.Done {
			mark = "yes"
		}
		tokens := "not measured"
		if r.Tokens >= 0 {
			tokens = fmt.Sprintf("%d", r.Tokens)
		}
		note := r.Note
		if r.Applied {
			note = strings.TrimSpace(note + " Applied.")
		}
		if r.Checks != "" {
			note = strings.TrimSpace(note + " Checks " + r.Checks + ".")
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %.1f | %s | %d | %d | %d | %s | %s |\n", r.Item, r.Kind, r.Arm, label, mark, r.Seconds, tokens,
			r.AtOnce, r.Agents, r.Computers, strings.ReplaceAll(note, "|", "/"), strings.ReplaceAll(r.Why, "|", "/"))
	}
	return b.String()
}
