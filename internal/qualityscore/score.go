// Package qualityscore reads the quality set's reports into the results rows
// people compare (docs/quality.md): the weekly run's, and those from a person's
// own computer (#1037).
package qualityscore

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
)

// Sets are the four a Score averages, and that a run on a person's computer
// measures, by the file each one's report is in.
var Sets = []Set{
	{Name: "quality", Report: "report.md"},
	{Name: "topics", Report: "topics.md"},
	{Name: "files", Report: "files.md"},
	{Name: "reasoning", Report: "reasoning.md"},
}

// Set is a set of cases and the report it writes.
type Set struct {
	Name   string
	Report string
}

// Passed is a set's score.
type Passed struct {
	Passed int `json:"passed"`
	Of     int `json:"of"`
}

// Row is one model on one computer on one date, as quality/results.json holds it.
type Row struct {
	Model              string            `json:"model"`
	Name               string            `json:"name"`
	Date               string            `json:"date"`
	Hardware           string            `json:"hardware"`
	OS                 string            `json:"os,omitempty"`
	MethodVersion      int               `json:"method_version"`
	Submitted          bool              `json:"submitted,omitempty"`
	SubmittedBy        string            `json:"submitted_by,omitempty"`
	OffList            bool              `json:"off_list,omitempty"`
	Source             string            `json:"source,omitempty"`
	Runs               string            `json:"runs,omitempty"`
	Why                string            `json:"why,omitempty"`
	Scores             map[string]Passed `json:"scores"`
	SecondsPerQuestion float64           `json:"seconds_per_question,omitempty"`
	Performance        *Performance      `json:"performance,omitempty"`
}

// How a model runs on a computer: all on its graphics card or chip; partly, with
// the rest in system memory, as a mixture-of-experts model's experts can be; on
// the processor alone; or not at all, which is a result worth publishing too.
const (
	RunsFull    = "full"
	RunsPartial = "partial"
	RunsCPU     = "cpu"
	RunsNo      = "no"
)

// PerformanceFile is the report of how a model ran, beside the sets' reports.
const PerformanceFile = "performance.json"

// Performance is how a model ran on the computer, as TestPerformance measures
// it: reading a long prompt apart from writing the answer, the wait from sending
// to the last token, and where it ran. It's beside a Score and never in it.
type Performance struct {
	// Runs and Why say how it ran, and Hardware where, for the row.
	Runs                     string  `json:"-"`
	Why                      string  `json:"-"`
	Hardware                 string  `json:"-"`
	OS                       string  `json:"-"`
	PromptTokens             int     `json:"prompt_tokens"`
	PromptTokensPerSecond    float64 `json:"prompt_tokens_per_second"`
	TokensPerSecond          float64 `json:"tokens_per_second"`
	FirstTokenMs             float64 `json:"first_token_ms"`
	EffectiveTokensPerSecond float64 `json:"effective_tokens_per_second"`
	Backend                  string  `json:"backend,omitempty"`
	GPULayers                int     `json:"gpu_layers"`
	GPULayersTotal           int     `json:"gpu_layers_total"`
	GPUMemoryBytes           uint64  `json:"gpu_memory_bytes,omitempty"`
	ContextWindow            int     `json:"context_window,omitempty"`
	Quantization             string  `json:"quantization,omitempty"`
	// LlamaCpp is the llama.cpp release it ran on, such as b12361 (#1066).
	LlamaCpp string `json:"llama_cpp,omitempty"`
	// Speculation is on when a draft guessed ahead during the runs, off
	// when nothing was guessed; DraftAccepted is the share of the guesses
	// the model kept (#835, #1066).
	Speculation   string  `json:"speculation,omitempty"`
	DraftAccepted float64 `json:"draft_accepted,omitempty"`
	// Devices are the GPUs it ran on, every one when there are several.
	Devices []string `json:"devices,omitempty"`
	// SystemMemoryBytes is the system memory the model's process held
	// after the runs, measured, beside GPUMemoryBytes (#1066). Where the
	// graphics share the computer's memory, as on a Mac, it includes what
	// GPUMemoryBytes counts: the two aren't added.
	SystemMemoryBytes uint64 `json:"system_memory_bytes,omitempty"`
}

// ReadPerformance reads a report, and says why when it can't be used: one for
// another model, or one that measured nothing, doesn't go in a row.
func ReadPerformance(path, model string) (*Performance, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var named struct {
		Model    string `json:"model"`
		Runs     string `json:"runs"`
		Why      string `json:"why"`
		Hardware string `json:"hardware"`
		OS       string `json:"os"`
	}
	var p Performance
	if err := json.Unmarshal(raw, &named); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if named.Model != model {
		return nil, fmt.Errorf("%s measured %s and not %s", path, named.Model, model)
	}
	p.Runs, p.Why, p.OS = named.Runs, named.Why, osName(named.OS)
	if named.Hardware != "" {
		p.Hardware = hardwareFrom(named.Hardware)
	}
	switch p.Runs {
	case RunsNo:
		if p.Why == "" {
			return nil, fmt.Errorf("%s says the model doesn't run, and not why", path)
		}
		return &p, nil
	case RunsFull, RunsPartial, RunsCPU:
	default:
		return nil, fmt.Errorf("%s doesn't say how the model ran", path)
	}
	if p.TokensPerSecond <= 0 || p.PromptTokensPerSecond <= 0 || p.FirstTokenMs <= 0 || p.EffectiveTokensPerSecond <= 0 {
		return nil, fmt.Errorf("%s has figures missing", path)
	}
	return &p, nil
}

// A report's own summary sentence, and not the first numbers in it: "25 of 25
// layers" read as a perfect score once.
var (
	casesPassed   = regexp.MustCompile(`(\d+) of (\d+) (?:cases|runs) passed`)
	scoreSentence = map[string]*regexp.Regexp{
		"reasoning": regexp.MustCompile(`(\d+) of (\d+) right`),
		"triage":    regexp.MustCompile(`type right on (\d+) of (\d+)`),
	}
	secondsSentence = regexp.MustCompile(`right \(\d+%\), ([\d.]+) s`)
	testedLine      = regexp.MustCompile(`(?m)^Tested Toskar .*? on (.+?), with ([^;\n]+?)(?:;.*|\.)?$`)
	memoryNote      = regexp.MustCompile(` \((\d+) GB\)$`)
)

// ScoreOf reads a set's score from its report, and false when it doesn't say.
func ScoreOf(set, report string) (Passed, bool) {
	sentence, ok := scoreSentence[set]
	if !ok {
		sentence = casesPassed
	}
	found := sentence.FindStringSubmatch(report)
	if found == nil {
		return Passed{}, false
	}
	passed, _ := strconv.Atoi(found[1])
	of, _ := strconv.Atoi(found[2])
	return Passed{Passed: passed, Of: of}, true
}

// SecondsPerQuestion reads the average from a reasoning report, or 0.
func SecondsPerQuestion(report string) float64 {
	found := secondsSentence.FindStringSubmatch(report)
	if found == nil {
		return 0
	}
	seconds, _ := strconv.ParseFloat(found[1], 64)
	return seconds
}

// Tested is what a report says it ran on.
type Tested struct {
	Model string
	// Hardware is the chip or card, with its memory when the report says.
	Hardware string
}

// A graphics card's driver name lists its whole family.
var cardNames = map[string]string{
	"AMD Radeon RX 7900 XT/7900 XTX/7900 GRE/7900M": "AMD RX 7900 XTX",
}

// CardName is the name results use for a graphics card or chip, so a computer's
// rows read the same whichever run made them.
func CardName(name string) string {
	if short, ok := cardNames[name]; ok {
		return short
	}
	return name
}

// Hardware is how a row names a computer: its graphics card or chip, and the
// memory a model can use there.
func Hardware(chip string, memoryGB uint64) string {
	return fmt.Sprintf("%s, %d GB", CardName(chip), memoryGB)
}

// osName is how results name an operating system.
func osName(goos string) string {
	switch goos {
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	case "windows":
		return "Windows"
	}
	return goos
}

// hardwareFrom names a computer from a report's words, such as "Some Card (24 GB)".
func hardwareFrom(name string) string {
	if memory := memoryNote.FindStringSubmatch(name); memory != nil {
		gb, _ := strconv.ParseUint(memory[1], 10, 64)
		return Hardware(memoryNote.ReplaceAllString(name, ""), gb)
	}
	return CardName(name)
}

// TestedOn reads a report's first line, and false when it has none. A run can
// end up with another model than it asked for, so a row is only as good as this.
func TestedOn(report string) (Tested, bool) {
	found := testedLine.FindStringSubmatch(report)
	if found == nil {
		return Tested{}, false
	}
	return Tested{Model: found[2], Hardware: hardwareFrom(found[1])}, true
}

// Score is a row's overall score: the plain average of the pass rates of the
// four Sets, from 0 to 100, and false for a row missing any. It's worked out
// when shown and not stored, so a new formula rewrites no data. Speed stays
// beside it: a fast wrong answer shouldn't beat a slow right one.
func Score(scores map[string]Passed) (int, bool) {
	total := 0.0
	for _, set := range Sets {
		s, ok := scores[set.Name]
		if !ok || s.Of == 0 {
			return 0, false
		}
		total += float64(s.Passed) / float64(s.Of)
	}
	return int(math.Round(100 * total / float64(len(Sets)))), true
}

// ReferenceModel is the model every computer's speed is compared on. It has to load
// on the phones' llama.cpp too, whose architectures are older than a computer's.
const ReferenceModel = "qwen2.5-3b-q4"

// ToskarScore is how fast a computer is for local AI, with no ceiling: its reading
// and writing speed on ReferenceModel against an M5 Pro with 24 GB at 1,000, as
// toskar.ai shows it. False for another model, whose speed is about that model too.
func ToskarScore(model string, p *Performance) (int, bool) {
	if model != ReferenceModel || p == nil {
		return 0, false
	}
	return int(math.Round(math.Sqrt(p.TokensPerSecond/104.3*(p.PromptTokensPerSecond/2732.2)) * 1000)), true
}

// Describe is a row's score as a person reads it, such as "95 (v1)".
func Describe(score, version int) string {
	return fmt.Sprintf("%d (v%d)", score, version)
}

var creditName = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,37}[a-zA-Z0-9])?$`)

// ValidCredit is whether a name can be a GitHub username: only that, so nobody can
// put a message in the data.
func ValidCredit(name string) bool { return creditName.MatchString(name) }
