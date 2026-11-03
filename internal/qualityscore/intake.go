package qualityscore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// A result someone sent arrives as an issue, from anyone. Check says whether a
// row can go in the public results with nobody reading it first. It can't prove
// a run was real: it refuses what's malformed, and holds anything unusual for a
// person. Text is the main risk on a public page, so names have to look like
// what they claim to be, and a row whose model isn't on the list always waits.

// MaxAutoPerDay is the most rows added without a person in a day. Past it, the
// rest wait, so a flood can't fill the table.
const MaxAutoPerDay = 20

// hardwareMakers begin a name that can be published without a person reading it.
var hardwareMakers = []string{
	"AMD", "Apple", "Intel", "NVIDIA", "Qualcomm", "Snapdragon", "MediaTek", "Google", "Samsung",
	"iPhone", "iPad", "Pixel", "Galaxy", "OnePlus", "Xiaomi", "Motorola", "Sony", "Nothing",
}

var (
	hardwareShape = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ./+-]{1,70}, \d{1,4} GB$`)
	osShape       = regexp.MustCompile(`^(Linux|macOS|Windows|iOS|iPadOS|Android)( [0-9][0-9.]{0,10})?$`)
	jsonFence     = regexp.MustCompile("(?s)```(?:json)?\\s*\n(.*?)\n\\s*```")
)

// expectedCases are how many cases each set has at the current method version.
// Quality runs one or two fewer on a model without vision.
var expectedCases = map[string][2]int{"quality": {60, 66}, "topics": {22, 22}, "files": {21, 21}, "reasoning": {39, 39}}

// IssueRows reads the rows out of an issue's body: its first JSON block, holding
// one row or a list of them.
func IssueRows(body string) ([]Row, error) {
	found := jsonFence.FindStringSubmatch(body)
	if found == nil {
		return nil, fmt.Errorf("no JSON block")
	}
	text := strings.TrimSpace(found[1])
	// The forms' field can wrap what was pasted in a second fence.
	if inner := strings.TrimSpace(strings.TrimPrefix(text, "```json")); inner != text {
		text = strings.TrimSpace(strings.TrimSuffix(inner, "```"))
	}
	var many []Row
	if strings.HasPrefix(text, "[") {
		if err := json.Unmarshal([]byte(text), &many); err != nil {
			return nil, err
		}
		if len(many) == 0 {
			return nil, fmt.Errorf("no results")
		}
		return many, nil
	}
	var one Row
	if err := json.Unmarshal([]byte(text), &one); err != nil {
		return nil, err
	}
	return []Row{one}, nil
}

// Check says why a row needs a person, or nothing when it's fine to publish.
func Check(row Row, list []ListedModel, published []Row, version int, now time.Time) []string {
	var why []string
	hold := func(format string, args ...any) { why = append(why, fmt.Sprintf(format, args...)) }

	listed := false
	for _, m := range list {
		if m.ID == row.Model {
			listed = true
			if row.Name != m.Name {
				hold("its name, %q, isn't the listed model's", row.Name)
			}
		}
	}
	if !listed {
		hold("%q isn't on the model list", row.Model)
	}
	if row.OffList {
		hold("it's marked as from outside the list")
	}
	if !knownHardware(row.Hardware) {
		hold("the computer's name, %q, doesn't look like one we know", row.Hardware)
	}
	if row.OS != "" && !osShape.MatchString(row.OS) {
		hold("the operating system, %q, doesn't look like one we know", row.OS)
	}
	if row.MethodVersion != version {
		hold("it's from method version %d, and the current one is %d", row.MethodVersion, version)
	}
	date, err := time.Parse("2006-01-02", row.Date)
	if err != nil || date.After(now.Add(36*time.Hour)) || date.Before(now.Add(-14*24*time.Hour)) {
		hold("its date, %q, isn't from the last two weeks", row.Date)
	}
	if row.Source != "" {
		hold("it names its own source")
	}

	switch row.Runs {
	case RunsNo:
		if !regexp.MustCompile(`^[a-z_]{1,40}$`).MatchString(row.Why) {
			hold("it says the model doesn't run, without a reason we know")
		}
		if len(row.Scores) > 0 || row.Performance != nil {
			hold("it says the model doesn't run, and has figures")
		}
	case RunsFull, RunsPartial, RunsCPU:
		if row.Performance == nil {
			hold("it has no speed figures")
		} else {
			why = append(why, checkPerformance(row)...)
		}
		why = append(why, checkScores(row.Scores)...)
	default:
		hold("it doesn't say how the model ran")
	}

	today, sameDay := now.UTC().Format("2006-01-02"), 0
	for _, p := range published {
		if p.Submitted && p.Date == today {
			sameDay++
		}
		if p.Model == row.Model && p.Hardware == row.Hardware && p.Date == row.Date && samePerformance(p.Performance, row.Performance) {
			hold("the same result is already there")
		}
	}
	if sameDay >= MaxAutoPerDay {
		hold("%d results already went in today", sameDay)
	}
	return why
}

// knownHardware is a name in a plain shape, beginning with a maker we know.
func knownHardware(name string) bool {
	if !hardwareShape.MatchString(name) {
		return false
	}
	gb, err := strconv.Atoi(memoryOf(name))
	if err != nil || gb < 1 || gb > 4096 {
		return false
	}
	first, _, _ := strings.Cut(name, " ")
	first = strings.TrimSuffix(first, ",")
	for _, maker := range hardwareMakers {
		if first == maker {
			return true
		}
	}
	return false
}

func memoryOf(name string) string {
	fields := strings.Fields(name)
	if len(fields) < 2 {
		return ""
	}
	return fields[len(fields)-2]
}

// checkPerformance looks for figures that can't come from a real run. The first
// token waits on reading the prompt, so those three have to agree, roughly.
func checkPerformance(row Row) []string {
	var why []string
	p := row.Performance
	if p.TokensPerSecond <= 0 || p.TokensPerSecond > 5000 {
		why = append(why, fmt.Sprintf("writing at %.1f tokens a second isn't believable", p.TokensPerSecond))
	}
	if p.PromptTokensPerSecond <= 0 || p.PromptTokensPerSecond > 200000 {
		why = append(why, fmt.Sprintf("reading at %.1f tokens a second isn't believable", p.PromptTokensPerSecond))
	}
	if p.PromptTokens < 3000 || p.PromptTokens > 10000 {
		why = append(why, fmt.Sprintf("a prompt of %d tokens isn't this test's", p.PromptTokens))
	}
	if p.EffectiveTokensPerSecond <= 0 || p.EffectiveTokensPerSecond > p.TokensPerSecond*1.05 {
		why = append(why, "start to finish is faster than it writes")
	}
	if p.PromptTokensPerSecond > 0 && p.PromptTokens > 0 {
		reading := float64(p.PromptTokens) / p.PromptTokensPerSecond * 1000
		if p.FirstTokenMs < reading*0.3 || p.FirstTokenMs > reading*4+5000 {
			why = append(why, fmt.Sprintf("a first token at %.0f ms doesn't fit reading %d tokens at %.0f a second", p.FirstTokenMs, p.PromptTokens, p.PromptTokensPerSecond))
		}
	}
	if p.GPULayers < 0 || p.GPULayers > p.GPULayersTotal {
		why = append(why, fmt.Sprintf("%d of %d layers on the GPU isn't possible", p.GPULayers, p.GPULayersTotal))
	}
	if row.Runs == RunsFull && p.GPULayersTotal > 0 && p.GPULayers != p.GPULayersTotal {
		why = append(why, "it says the model ran fully, with layers off the GPU")
	}
	return why
}

// checkScores takes all four sets, at their real sizes, or a speed test with none.
func checkScores(scores map[string]Passed) []string {
	if len(scores) == 0 {
		return nil
	}
	var why []string
	if _, complete := Score(scores); !complete {
		why = append(why, "it has some of the sets and not all")
	}
	for set, s := range scores {
		size, known := expectedCases[set]
		switch {
		case !known:
			why = append(why, fmt.Sprintf("%q isn't a set", set))
		case s.Of < size[0] || s.Of > size[1]:
			why = append(why, fmt.Sprintf("%s has %d cases, and this version has about %d", set, s.Of, size[0]))
		case s.Passed < 0 || s.Passed > s.Of:
			why = append(why, fmt.Sprintf("%s passed %d of %d", set, s.Passed, s.Of))
		}
	}
	return why
}

func samePerformance(a, b *Performance) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.TokensPerSecond == b.TokensPerSecond && a.PromptTokensPerSecond == b.PromptTokensPerSecond && a.FirstTokenMs == b.FirstTokenMs
}

// Verdict is what intake found: every row passing, or why it needs a person.
type Verdict struct {
	Passed bool     `json:"passed"`
	Why    []string `json:"why,omitempty"`
	Rows   []Row    `json:"rows,omitempty"`
}

// Intake checks an issue's rows, all of them or none. A username counts only when
// it's who opened the issue, so nobody can post as someone else: anything else
// shows as Community.
func Intake(body, author, source string, list []ListedModel, published []Row, version int, now time.Time) Verdict {
	rows, err := IssueRows(body)
	if err != nil {
		return Verdict{Why: []string{"its result couldn't be read: " + err.Error()}}
	}
	verdict := Verdict{Passed: true}
	for _, row := range rows {
		for _, why := range Check(row, list, published, version, now) {
			verdict.Why = append(verdict.Why, plainReason(row.Name+": "+why))
		}
		row.Submitted, row.Source = true, source
		if !strings.EqualFold(row.SubmittedBy, author) || !ValidCredit(row.SubmittedBy) {
			row.SubmittedBy = ""
		}
		verdict.Rows = append(verdict.Rows, row)
	}
	verdict.Passed = len(verdict.Why) == 0
	return verdict
}

// plainReason keeps a reason from carrying a mention, link, or markup into the
// comment the workflow posts: it can quote what anyone wrote in the issue.
func plainReason(reason string) string {
	if len(reason) > 300 {
		reason = reason[:300]
	}
	return strings.NewReplacer("@", "", "<", "", ">", "", "[", "", "]", "", "`", "", "http", "", "\n", " ").Replace(reason)
}

// AddRows appends rows to the results file, one line each, leaving the rest as it is.
func AddRows(resultsPath string, rows []Row) error {
	raw, err := os.ReadFile(resultsPath)
	if err != nil {
		return err
	}
	end := bytes.LastIndex(raw, []byte("\n  ]"))
	if end < 0 {
		return fmt.Errorf("%s: no list of results to add to", resultsPath)
	}
	added := ""
	for _, row := range rows {
		line, err := RowLine(row)
		if err != nil {
			return err
		}
		added += ",\n    " + line
	}
	return os.WriteFile(resultsPath, []byte(string(raw[:end])+added+string(raw[end:])), 0o644)
}
