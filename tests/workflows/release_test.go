package workflows

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// A pressed Release run has no tag in github.ref_name: it is on main, and the
// version job picks the tag. Every job after that job reads the tag from it,
// or a pressed release would publish "main".
func TestReleaseJobsReadTheTagFromTheVersionJob(t *testing.T) {
	path := filepath.Join("..", "..", ".github", "workflows", "release.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var wf struct {
		Jobs map[string]yaml.Node `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &wf); err != nil {
		t.Fatal(err)
	}
	if _, ok := wf.Jobs["version"]; !ok {
		t.Fatal("release.yml has no version job")
	}
	for name, node := range wf.Jobs {
		if name == "version" {
			continue
		}
		out, err := yaml.Marshal(&node)
		if err != nil {
			t.Fatal(err)
		}
		text := string(out)
		for _, ref := range []string{"github.ref_name", "GITHUB_REF_NAME", "github.ref "} {
			if strings.Contains(text, ref) {
				t.Errorf("job %s uses %s; use needs.version.outputs.tag or $TAG", name, ref)
			}
		}
		if strings.Contains(text, "needs.version.outputs.tag") {
			var job struct {
				Needs any `yaml:"needs"`
			}
			if err := node.Decode(&job); err != nil {
				t.Fatal(err)
			}
			if !needsJob(job.Needs, "version") {
				t.Errorf("job %s reads needs.version.outputs.tag but doesn't need version", name)
			}
		}
	}
}

func needsJob(needs any, want string) bool {
	switch v := needs.(type) {
	case string:
		return v == want
	case []any:
		for _, n := range v {
			if n == want {
				return true
			}
		}
	}
	return false
}
