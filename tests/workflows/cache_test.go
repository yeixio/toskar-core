// Package workflows checks the repository's GitHub Actions workflows.
package workflows

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type step struct {
	Uses string         `yaml:"uses"`
	If   string         `yaml:"if"`
	Run  string         `yaml:"run"`
	With map[string]any `yaml:"with"`
}

type job struct {
	RunsOn any    `yaml:"runs-on"`
	Steps  []step `yaml:"steps"`
}

// gitHubHosted says a job can only run on GitHub's machines. Anything chosen by
// a variable or a matrix may run on a home runner.
func gitHubHosted(runsOn any) bool {
	name, ok := runsOn.(string)
	return ok && !strings.Contains(name, "${{") && strings.HasSuffix(name, "-latest")
}

// guarded says a value turns a cache off on self-hosted runners.
func guarded(value string) bool {
	return strings.Contains(value, "github-hosted") || strings.Contains(value, "!vars.CI_RUNS_ON")
}

// Restoring a cache from GitHub on a home runner sends it over that runner's
// internet connection, and saving one sends it back, for files that could stay
// on its disk (#1081). setup-go caches unless told not to.
func TestHomeRunnersDoNotUseGitHubsCache(t *testing.T) {
	files, err := filepath.Glob("../../.github/workflows/*.yml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no workflows found: %v", err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var workflow struct {
			Jobs map[string]job `yaml:"jobs"`
		}
		if err := yaml.Unmarshal(data, &workflow); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		name := filepath.Base(file)
		for jobName, j := range workflow.Jobs {
			if gitHubHosted(j.RunsOn) {
				continue
			}
			for _, s := range j.Steps {
				action, _, _ := strings.Cut(s.Uses, "@")
				switch action {
				case "actions/cache", "actions/cache/restore", "actions/cache/save":
					if !guarded(s.If) {
						t.Errorf("%s, job %s: %s needs if: runner.environment == 'github-hosted'", name, jobName, s.Uses)
					}
				case "actions/setup-go", "actions/setup-node", "actions/setup-java":
					cache, set := s.With["cache"]
					if !set && action != "actions/setup-go" {
						continue
					}
					value, isString := cache.(string)
					if off, isBool := cache.(bool); isBool && !off {
						continue
					}
					if !isString || !guarded(value) {
						t.Errorf("%s, job %s: %s needs cache: ${{ runner.environment == 'github-hosted' }}", name, jobName, s.Uses)
					}
				}
				if strings.Contains(s.Run, "TOSKAR_BENCHMARK_DIR=$RUNNER_TEMP") {
					t.Errorf("%s, job %s: the benchmark's folder is deleted with RUNNER_TEMP, so its model is downloaded again", name, jobName)
				}
			}
		}
	}
}
