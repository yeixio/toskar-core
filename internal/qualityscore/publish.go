package qualityscore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const reportFolderPrefix = "quality-report-real-models-"

// Publish adds a scheduled run's scores to results.json. The reports folder
// holds a quality-report-real-models-<model> folder per model, as the Quality
// workflow uploads them. A model gets a row only when its reports say they
// tested that model: "recommended" stands for several, and a report naming
// another means the run didn't test the one it was asked to. Only a complete
// row is published, with a score from every one of Sets and its performance,
// so every row can be compared and has a Score. Publishing the same run again adds nothing. It says
// what it did on log.
func Publish(reports, resultsPath string, version int, names map[string]string, source string, now time.Time, log func(string)) error {
	raw, err := os.ReadFile(resultsPath)
	if err != nil {
		return err
	}
	var results struct {
		Results []Row `json:"results"`
	}
	if err := json.Unmarshal(raw, &results); err != nil {
		return fmt.Errorf("%s: %w", resultsPath, err)
	}
	done := map[string]bool{}
	for _, r := range results.Results {
		if r.Source == source {
			done[r.Model] = true
		}
	}
	folders, err := filepath.Glob(filepath.Join(reports, reportFolderPrefix+"*"))
	if err != nil {
		return err
	}
	sort.Strings(folders)
	added := ""
	for _, folder := range folders {
		model := strings.TrimPrefix(filepath.Base(folder), reportFolderPrefix)
		if done[model] {
			continue
		}
		name := names[model]
		if name == "" {
			name = model
		}
		row, why := weeklyRow(model, folder)
		if why != "" {
			log(fmt.Sprintf("No row for %s: %s.", model, why))
			continue
		}
		row.Name, row.MethodVersion, row.Source = name, version, source
		row.Date = now.UTC().Format("2006-01-02")
		line, err := RowLine(row)
		if err != nil {
			return err
		}
		log(line)
		added += ",\n    " + line
	}
	if added == "" {
		return nil
	}
	// Each row stays on its own line, so the file's history shows what was added.
	end := bytes.LastIndex(raw, []byte("\n  ]"))
	if end < 0 {
		return fmt.Errorf("%s: no list of results to add to", resultsPath)
	}
	out := string(raw[:end]) + added + string(raw[end:])
	return os.WriteFile(resultsPath, []byte(out), 0o644)
}

// weeklyRow is a model's row, without what its folder doesn't say, or why it has none.
func weeklyRow(model, folder string) (Row, string) {
	row := Row{Model: model, Scores: map[string]Passed{}}
	for _, set := range Sets {
		raw, err := os.ReadFile(filepath.Join(folder, set.Report))
		if err != nil {
			continue
		}
		tested, ok := TestedOn(string(raw))
		if !ok {
			continue
		}
		if tested.Model != model {
			return row, "its report says it tested " + tested.Model
		}
		if row.Hardware == "" {
			row.Hardware = tested.Hardware
		}
		if score, ok := ScoreOf(set.Name, string(raw)); ok {
			row.Scores[set.Name] = score
		}
		if set.Name == "reasoning" {
			row.SecondsPerQuestion = SecondsPerQuestion(string(raw))
		}
	}
	if _, complete := Score(row.Scores); !complete {
		return row, "its reports don't have a score from all of " + setNames()
	}
	performance, err := ReadPerformance(filepath.Join(folder, PerformanceFile), model)
	if err != nil {
		return row, "no performance figures: " + err.Error()
	}
	row.Performance = performance
	return row, ""
}

func setNames() string {
	names := make([]string, len(Sets))
	for i, set := range Sets {
		names[i] = set.Name
	}
	return strings.Join(names, ", ")
}

// RowLine is a row in the results file's own style: one line, with spaces after
// its colons and commas.
func RowLine(row Row) (string, error) {
	var compact bytes.Buffer
	enc := json.NewEncoder(&compact)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(row); err != nil {
		return "", err
	}
	var out strings.Builder
	inString, escaped := false, false
	for _, r := range strings.TrimSpace(compact.String()) {
		out.WriteRune(r)
		switch {
		case escaped:
			escaped = false
		case inString && r == '\\':
			escaped = true
		case r == '"':
			inString = !inString
		case !inString && (r == ':' || r == ','):
			out.WriteRune(' ')
		}
	}
	return out.String(), nil
}
