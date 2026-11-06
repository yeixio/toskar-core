package quality

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/config"
)

// projectFolder writes a case's project files to a folder of its own, and
// gives its real path: macOS's temp folder is behind a link. With
// TOSKAR_QUALITY_PROJECTS, it's made there: a folder a daemon in another
// container sees at the same path.
func projectFolder(t *testing.T, files map[string]string) string {
	t.Helper()
	base := config.Env("QUALITY_PROJECTS")
	if base == "" {
		base = t.TempDir()
	}
	dir, err := os.MkdirTemp(base, "project-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

// tallyProject has a one-line bug: the loop starts at 1, so Sum leaves out
// the first number, and the test that expects 9 gets 7.
var tallyProject = map[string]string{
	"go.mod": "module example.com/tally\n\ngo 1.22\n",
	"tally.go": "package tally\n\n// Sum adds up the numbers.\nfunc Sum(nums []int) int {\n\ttotal := 0\n" +
		"\tfor i := 1; i < len(nums); i++ {\n\t\ttotal += nums[i]\n\t}\n\treturn total\n}\n",
	"tally_test.go": "package tally\n\nimport \"testing\"\n\nfunc TestSum(t *testing.T) {\n" +
		"\tif got := Sum([]int{2, 3, 4}); got != 9 {\n\t\tt.Fatalf(\"Sum = %d, want 9\", got)\n\t}\n}\n",
}

// A question about the files in a chat's folder is answered by reading them,
// not guessed (#1101): the answer names the real cause, and nothing changes.
// It isn't in cases.json, whose score has a version, and whose copy the phone
// app keeps.
func TestFolderQuestion(t *testing.T) {
	d, _ := qualityDriver(t)
	c := Case{
		ID:      "folder_question_reads_files",
		What:    "A question about a file in the chat's folder reads it and names the real cause.",
		Setup:   Setup{Project: tallyProject},
		Message: "Read tally.go and tally_test.go and tell me why the test fails. Don't change anything yet.",
		Stub: []string{
			`{"tool_call":{"id":"filesystem.read","args":{"path":"tally.go"}}}`,
			`{"tool_call":{"id":"filesystem.read","args":{"path":"tally_test.go"}}}`,
			"The loop in Sum starts at 1, so it skips the first number: Sum([]int{2, 3, 4}) is 7, and the test wants 9. Starting the loop at 0 fixes it.",
		},
		Expect: Expect{
			ToolsRun: []string{"filesystem.read"},
			NotRun:   []string{"filesystem.write", "terminal"},
			Sources:  []string{"file"},
			// The cause, however it's put: the loop's start, or the first number left out.
			AnswerMatches: `(?is)(i\s*:=\s*1|starts? (at|from) (1|one)|(skip|miss|leave|leaves|left|ignor|exclud|omit)\w*\b.{0,40}\b(first|2)\b|index 0|nums\[0\])`,
		},
	}
	if stopped(t, d, c) {
		return
	}
	r := d.Run(t, c)
	for _, failure := range check(d.Name(), c, r, nil) {
		t.Error(failure)
	}
	if strings.TrimSpace(r.Answer) == "" {
		t.Error("no answer")
	}
}
