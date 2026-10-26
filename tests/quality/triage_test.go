package quality

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/askdata"
	"github.com/yeixio/toskar-core/internal/config"
)

// The triage set (#455): issues of this repository, as written, with the
// labels maintainers gave them, to measure how well a model suggests them
// the way the Triage workflow (.github/workflows/triage.yml) asks. The
// workflow's labels, prompt, and limits are in .github/triage.json, which
// both read. A real model's numbers are measured, not held to a pass rate:
// the labels are sparse, so a suggestion they don't have isn't always
// wrong.

const triageConfigPath = "../../.github/triage.json"

// triageMaxBytes is how much of an issue the workflow sends, as its
// head -c does, so a crash report's stack fits the model's context.
const triageMaxBytes = 12000

// triageConfig is .github/triage.json.
type triageConfig struct {
	Prompt string        `json:"prompt"`
	Groups []triageGroup `json:"groups"`
}

// triageGroup is a group of labels: exactly one without Max, at most Max
// with it.
type triageGroup struct {
	Name   string            `json:"name"`
	Ask    string            `json:"ask"`
	Max    int               `json:"max"`
	Labels map[string]string `json:"labels"`
	// order is the labels as the file lists them, for the prompt.
	order []string
}

type triageCase struct {
	Issue  int      `json:"issue"`
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Labels []string `json:"labels"`
}

func loadTriageConfig(t *testing.T) triageConfig {
	t.Helper()
	raw, err := os.ReadFile(triageConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	var cfg triageConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	// The labels in the file's order, as jq's to_entries keeps it.
	var doc struct {
		Groups []struct {
			Labels json.RawMessage `json:"labels"`
		} `json:"groups"`
	}
	_ = json.Unmarshal(raw, &doc)
	for i := range cfg.Groups {
		dec := json.NewDecoder(strings.NewReader(string(doc.Groups[i].Labels)))
		_, _ = dec.Token()
		for dec.More() {
			key, _ := dec.Token()
			var desc string
			_ = dec.Decode(&desc)
			cfg.Groups[i].order = append(cfg.Groups[i].order, key.(string))
		}
	}
	return cfg
}

func loadTriageCases(t *testing.T) []triageCase {
	t.Helper()
	raw, err := os.ReadFile("triage.json")
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Cases []triageCase `json:"cases"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Cases) == 0 {
		t.Fatal("triage.json has no cases")
	}
	return file.Cases
}

// prompt is what the workflow asks: the prompt, then each group's ask and
// its labels with what they mean.
func (c triageConfig) prompt() string {
	lines := []string{c.Prompt}
	for _, g := range c.Groups {
		lines = append(lines, g.Ask)
		for _, l := range g.order {
			lines = append(lines, "- "+l+": "+g.Labels[l])
		}
	}
	return strings.Join(lines, "\n")
}

// schema is the answer's JSON Schema, as the workflow builds it.
func (c triageConfig) schema() map[string]any {
	props := map[string]any{}
	var required []string
	for _, g := range c.Groups {
		enum := make([]string, 0, len(g.Labels))
		for l := range g.Labels {
			enum = append(enum, l)
		}
		sort.Strings(enum)
		if g.Max > 0 {
			props[g.Name] = map[string]any{"type": "array", "items": map[string]any{"enum": enum}, "maxItems": g.Max}
		} else {
			props[g.Name] = map[string]any{"enum": enum}
		}
		required = append(required, g.Name)
	}
	return map[string]any{"type": "object", "properties": props, "required": required}
}

// labels keeps the answer's labels each group allows, as the workflow's
// comment job does: never anything else the answer says.
func (c triageConfig) labels(answer map[string]any) []string {
	var out []string
	for _, g := range c.Groups {
		var got []any
		switch v := answer[g.Name].(type) {
		case []any:
			got = v
		case nil:
		default:
			got = []any{v}
		}
		for _, l := range got {
			if s, ok := l.(string); ok {
				if _, allowed := g.Labels[s]; allowed && !slices.Contains(out, s) {
					out = append(out, s)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// typeOf is the label of the single-choice group, the issue's type.
func (c triageConfig) typeOf(labels []string) []string {
	var out []string
	for _, g := range c.Groups {
		if g.Max == 0 {
			for _, l := range labels {
				if _, ok := g.Labels[l]; ok {
					out = append(out, l)
				}
			}
		}
	}
	return out
}

// TestTriageCases checks the set and how the workflow's question is built:
// every case's labels are ones the workflow may suggest, each has a type,
// and the prompt, schema, and filter work as the workflow's jq does.
func TestTriageCases(t *testing.T) {
	cfg := loadTriageConfig(t)
	allowed := map[string]bool{}
	for _, g := range cfg.Groups {
		if g.Name == "" || g.Ask == "" || len(g.Labels) == 0 || len(g.order) != len(g.Labels) {
			t.Errorf("group %q: needs a name, an ask, and labels", g.Name)
		}
		for l := range g.Labels {
			if allowed[l] {
				t.Errorf("%s is in two groups", l)
			}
			allowed[l] = true
		}
	}
	seen := map[int]bool{}
	for _, c := range loadTriageCases(t) {
		if seen[c.Issue] {
			t.Errorf("#%d: twice", c.Issue)
		}
		seen[c.Issue] = true
		if c.Title == "" {
			t.Errorf("#%d: no title", c.Issue)
		}
		for _, l := range c.Labels {
			if !allowed[l] {
				t.Errorf("#%d: %s isn't a label the workflow may suggest", c.Issue, l)
			}
		}
		if len(cfg.typeOf(c.Labels)) == 0 {
			t.Errorf("#%d: no type label", c.Issue)
		}
	}

	prompt := cfg.prompt()
	if !strings.HasPrefix(prompt, cfg.Prompt+"\n"+cfg.Groups[0].Ask+"\n- bug: ") {
		t.Errorf("the prompt starts %q", prompt[:min(len(prompt), 120)])
	}
	schema, _ := json.Marshal(cfg.schema())
	for _, want := range []string{`"required":["type","area","platform"]`, `"type":{"enum":["bug","documentation","enhancement","question","research"]}`, `"maxItems":2`} {
		if !strings.Contains(string(schema), want) {
			t.Errorf("the schema lacks %s: %s", want, schema)
		}
	}
	for answer, want := range map[string]string{
		`{"type":"bug","area":["runtime","wontfix"],"platform":["nvidia"]}`: "bug,nvidia,runtime",
		`{"type":"wontfix","area":"api"}`:                                   "api",
		`{"area":[1,"cli","cli"]}`:                                          "cli",
		`{"type":"enhancement","area":["bug"]}`:                             "enhancement",
	} {
		var a map[string]any
		_ = json.Unmarshal([]byte(answer), &a)
		if got := strings.Join(cfg.labels(a), ","); got != want {
			t.Errorf("labels(%s) = %q, want %q", answer, got, want)
		}
	}
	// The workflow builds the same from the same fields.
	workflow, err := os.ReadFile("../../.github/workflows/triage.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{fmt.Sprintf("head -c %d", triageMaxBytes), ".prompt, (.groups[] | .ask,", "if .max then", "maxItems: .max", "required: [.groups[].name]", "$cfg[0].groups[]"} {
		if !strings.Contains(string(workflow), field) {
			t.Errorf("triage.yml no longer builds its question with %q; keep it and this test in step", field)
		}
	}
}

// firstBytes is the start of s, at most n bytes.
func firstBytes(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// triageResult is one issue's outcome, for the reports.
type triageResult struct {
	Issue     int      `json:"issue"`
	Want      []string `json:"want"`
	Got       []string `json:"got"`
	TypeRight bool     `json:"type_right"`
	Seconds   float64  `json:"seconds"`
	Error     string   `json:"error,omitempty"`
}

// TestTriageQuality asks a daemon to label every issue in the set, as the
// workflow does, and reports how often the type was right, the share of
// suggestions maintainers gave (precision) and of their labels it found
// (recall), and the labels it added or missed most. TOSKAR_QUALITY_TRIAGE_MODEL
// picks the model (the run's model, or auto); TOSKAR_QUALITY_TRIAGE_REPORT
// writes a Markdown report and TOSKAR_QUALITY_TRIAGE_JSON the results.
func TestTriageQuality(t *testing.T) {
	cfg := loadTriageConfig(t)
	cases := loadTriageCases(t)
	d, build := qualityDriver(t)
	real, ok := d.(realDriver)
	if !ok {
		t.Skip("needs a daemon with a model (TOSKAR_QUALITY_URL); TestTriageCases checks the set")
	}
	model := config.Env("QUALITY_TRIAGE_MODEL")
	if model == "" {
		model = real.modelID()
	} else {
		build = real.version(t) + " on " + real.hardware(t) + ", with " + model
	}
	prompt := cfg.prompt()
	format := map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "answer", "schema": cfg.schema()}}
	var results []triageResult
	for _, c := range cases {
		if real.Stopped() != "" {
			t.Errorf("stopped before #%d: %s", c.Issue, real.Stopped())
			break
		}
		res := triageResult{Issue: c.Issue, Want: c.Labels}
		ran := false
		t.Run(fmt.Sprint(c.Issue), func(t *testing.T) {
			ran = true
			body := map[string]any{
				"model":           model,
				"messages":        []map[string]string{{"role": "user", "content": askdata.Message(prompt, firstBytes(c.Title+"\n\n"+c.Body, triageMaxBytes))}},
				"toskar":          map[string]any{"memory": false, "knowledge": false, "tools": []string{}},
				"temperature":     0,
				"response_format": format,
			}
			var reply struct {
				Choices []struct {
					Message struct {
						Content string `json:"content"`
					} `json:"message"`
				} `json:"choices"`
			}
			started := time.Now()
			_, err := real.request(http.MethodPost, "/v1/chat/completions", body, &reply, t.Logf)
			res.Seconds = time.Since(started).Seconds()
			if err != nil || len(reply.Choices) == 0 {
				res.Error = fmt.Sprintf("no answer: %v", err)
				t.Log(res.Error)
				return
			}
			var answer map[string]any
			if err := json.Unmarshal([]byte(reply.Choices[0].Message.Content), &answer); err != nil {
				res.Error = "not JSON: " + reply.Choices[0].Message.Content
				t.Log(res.Error)
				return
			}
			res.Got = cfg.labels(answer)
			res.TypeRight = len(cfg.typeOf(res.Got)) > 0 && slices.Contains(c.Labels, cfg.typeOf(res.Got)[0])
			t.Logf("#%d: suggested %v, labeled %v", c.Issue, res.Got, c.Labels)
		})
		if ran {
			results = append(results, res)
		}
	}
	sum := summarizeTriage(results)
	t.Logf("triage with %s: %s", model, sum.line())
	if path := config.Env("QUALITY_TRIAGE_REPORT"); path != "" {
		writeReport(t, d, build, path, triageReport(model, results, sum))
	}
	if path := config.Env("QUALITY_TRIAGE_JSON"); path != "" {
		out, _ := json.MarshalIndent(map[string]any{"model": model, "build": build, "summary": sum, "cases": results}, "", "  ")
		if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
			t.Errorf("results: %v", err)
		}
	}
}

// triageSummary is how the set went.
type triageSummary struct {
	Cases     int            `json:"cases"`
	Answered  int            `json:"answered"`
	TypeRight int            `json:"type_right"`
	Right     int            `json:"right"`
	Extra     int            `json:"extra"`
	Missed    int            `json:"missed"`
	Precision float64        `json:"precision"`
	Recall    float64        `json:"recall"`
	Seconds   float64        `json:"mean_seconds"`
	Added     map[string]int `json:"added"`
	Left      map[string]int `json:"left_out"`
}

func summarizeTriage(results []triageResult) triageSummary {
	s := triageSummary{Cases: len(results), Added: map[string]int{}, Left: map[string]int{}}
	for _, r := range results {
		if r.Error != "" {
			continue
		}
		s.Answered++
		s.Seconds += r.Seconds
		if r.TypeRight {
			s.TypeRight++
		}
		for _, l := range r.Got {
			if slices.Contains(r.Want, l) {
				s.Right++
			} else {
				s.Extra++
				s.Added[l]++
			}
		}
		for _, l := range r.Want {
			if !slices.Contains(r.Got, l) {
				s.Missed++
				s.Left[l]++
			}
		}
	}
	s.Precision = share(s.Right, s.Right+s.Extra)
	s.Recall = share(s.Right, s.Right+s.Missed)
	if s.Answered > 0 {
		s.Seconds /= float64(s.Answered)
	}
	return s
}

func (s triageSummary) line() string {
	return fmt.Sprintf("type right on %d of %d, precision %.0f%%, recall %.0f%%, %.1f s an issue (%d answered of %d)",
		s.TypeRight, s.Answered, s.Precision*100, s.Recall*100, s.Seconds, s.Answered, s.Cases)
}

// most is a count's labels, most first.
func most(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if counts[keys[i]] != counts[keys[j]] {
			return counts[keys[i]] > counts[keys[j]]
		}
		return keys[i] < keys[j]
	})
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, counts[k]))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

// triageReport is the Markdown report: the numbers, then every issue.
func triageReport(model string, results []triageResult, s triageSummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Triage: core, %s\n\n", model)
	fmt.Fprintf(&b, "%s.\n\n", s.line())
	fmt.Fprintf(&b, "Added most: %s.\n\nLeft out most: %s.\n\n", most(s.Added), most(s.Left))
	b.WriteString("| Issue | Labeled | Suggested | Seconds |\n| --- | --- | --- | --- |\n")
	for _, r := range results {
		got := strings.Join(r.Got, ", ")
		if r.Error != "" {
			got = "**" + strings.ReplaceAll(r.Error, "|", "\\|") + "**"
			if len(got) > 100 {
				got = got[:100] + "…**"
			}
		}
		fmt.Fprintf(&b, "| #%d | %s | %s | %.1f |\n", r.Issue, strings.Join(r.Want, ", "), got, r.Seconds)
	}
	return b.String()
}
