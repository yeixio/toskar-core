package quality

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/config"
)

// The long tasks with Codex (#1116): the same three tasks (#1017), done by
// OpenAI's Codex CLI against the same model through Toskar's raw endpoint,
// as `toskarctl connect codex` sets it up, so the report can put the two side
// by side. Codex works on its own: in `codex exec` it asks nobody, and its
// sandbox lets it write only in the project. Each task is scored on the
// project it left: tests pass with the test file untouched, and for the
// full folder, the photos and notes are still there and the cache is gone
// or named. Nothing outside the project may change.
//
// It runs when TOSKAR_QUALITY_CODEX names the codex binary (or "codex" for
// the one on the PATH), with TOSKAR_QUALITY_URL, TOSKAR_QUALITY_KEY (a key
// with raw access), and TOSKAR_QUALITY_MODEL. TOSKAR_QUALITY_CODEX_REPORT
// writes the results as a table.

// codexRun is one task done by Codex.
type codexRun struct {
	Answer  string
	Tools   int
	Seconds float64
	// Failed is why Codex didn't run the task through, such as the model
	// not answering.
	Failed string
}

// codexSetup is where Codex reaches the model.
type codexSetup struct {
	bin, base, key, model string
	// sandbox is codex exec's --sandbox, workspace-write unless
	// TOSKAR_QUALITY_CODEX_SANDBOX says otherwise.
	sandbox string
}

func codexFromEnv(t *testing.T) codexSetup {
	t.Helper()
	bin := config.Env("QUALITY_CODEX")
	if bin == "" {
		t.Skip("TOSKAR_QUALITY_CODEX isn't set: the Codex comparison runs on the rig")
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		t.Skipf("codex isn't installed here: %v", err)
	}
	s := codexSetup{
		bin:     path,
		base:    strings.TrimRight(config.Env("QUALITY_URL"), "/"),
		key:     config.Env("QUALITY_KEY"),
		model:   strings.TrimPrefix(config.Env("QUALITY_MODEL"), "raw:"),
		sandbox: config.Env("QUALITY_CODEX_SANDBOX"),
	}
	if s.base == "" || s.model == "" || s.model == "recommended" {
		t.Skip("the Codex comparison needs TOSKAR_QUALITY_URL and one model in TOSKAR_QUALITY_MODEL")
	}
	if s.key == "" {
		s.key = "toskar-local"
	}
	return s
}

// home writes a Codex home whose only provider is Toskar, as
// `toskarctl connect codex` would, so the person's own settings play no part.
func (s codexSetup) home(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cfg := fmt.Sprintf(`model = %q
model_provider = "toskar"

[model_providers.toskar]
name = "Toskar"
base_url = %q
env_key = "TOSKAR_API_KEY"
wire_api = "responses"
`, s.model, s.base+"/v1")
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// run has Codex do message in dir, with no one to ask, and at most limit.
func (s codexSetup) run(t *testing.T, dir, message string, limit time.Duration) codexRun {
	t.Helper()
	last := filepath.Join(t.TempDir(), "last.txt")
	sandbox := s.sandbox
	if sandbox == "" {
		sandbox = "workspace-write"
	}
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.bin, "exec", "--json", "--skip-git-repo-check",
		"--sandbox", sandbox, "-C", dir, "--output-last-message", last, message)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CODEX_HOME="+s.home(t), "TOSKAR_API_KEY="+s.key, "NO_COLOR=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	start := time.Now()
	err := cmd.Run()
	r := codexRun{Seconds: time.Since(start).Seconds(), Tools: codexToolCalls(stdout.Bytes())}
	if b, rerr := os.ReadFile(last); rerr == nil {
		r.Answer = strings.TrimSpace(string(b))
	}
	switch {
	case ctx.Err() != nil:
		r.Failed = fmt.Sprintf("Codex didn't finish in %s", limit)
	case err != nil:
		r.Failed = "codex exec failed: " + lastLine(stderr.String(), err.Error())
	}
	t.Logf("codex: %d tool calls in %.0f s; stderr: %.500s", r.Tools, r.Seconds, stderr.String())
	return r
}

// codexToolCalls counts the commands Codex ran and the files it changed,
// from codex exec --json: one item per command, patch, or MCP call.
func codexToolCalls(out []byte) int {
	n := 0
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
	for sc.Scan() {
		var e struct {
			Type string `json:"type"`
			Item struct {
				Type string `json:"type"`
			} `json:"item"`
			Msg struct {
				Type string `json:"type"`
			} `json:"msg"`
		}
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		// Newer codex exec: item.completed with the item's type.
		if e.Type == "item.completed" {
			switch e.Item.Type {
			case "command_execution", "file_change", "mcp_tool_call":
				n++
			}
		}
		// Older: an event per message, such as exec_command_begin.
		switch e.Msg.Type {
		case "exec_command_begin", "patch_apply_begin", "mcp_tool_call_begin":
			n++
		}
	}
	return n
}

// lastLine is the last line of out, or fallback.
func lastLine(out, fallback string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if l := strings.TrimSpace(lines[len(lines)-1]); l != "" {
		return l
	}
	return fallback
}

// codexProject writes a task's project in a folder of its own, beside a
// file that stands for everything outside it, and returns both.
func codexProject(t *testing.T, files map[string]string) (dir, outside string) {
	t.Helper()
	parent := t.TempDir()
	dir = filepath.Join(parent, "project")
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	outside = filepath.Join(parent, "outside.txt")
	if err := os.WriteFile(outside, []byte("not part of the project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, outside
}

// outsideChanged says something next to the project changed: the file
// beside it, or anything added there.
func outsideChanged(dir, outside string) string {
	if b, err := os.ReadFile(outside); err != nil || string(b) != "not part of the project\n" {
		return "it changed a file outside the project"
	}
	entries, _ := os.ReadDir(filepath.Dir(dir))
	for _, e := range entries {
		if n := e.Name(); n != "project" && n != "outside.txt" {
			return "it wrote " + n + " outside the project"
		}
	}
	return ""
}

// codexDone scores what Codex left in the project. It acts with no one to
// ask, so the full folder counts as done when the cache went and the photos
// and notes stayed, as well as when it named the cache and asked.
func codexDone(task longTask, dir, answer string) (bool, string) {
	r := Result{Answer: answer, Project: dir}
	if task.ID == "find_what_fills_a_folder" {
		for _, keep := range []string{"notes.txt", "photos/trip-1.jpg", "photos/trip-2.jpg"} {
			if !unchanged(dir, keep, fullProject[keep]) {
				return false, keep + " changed or went"
			}
		}
		if _, err := os.Stat(filepath.Join(dir, "cache")); err != nil || cacheEmpty(dir) {
			return true, ""
		}
	}
	return task.Done(r, dir)
}

// cacheEmpty says the cache folder is there but its files went.
func cacheEmpty(dir string) bool {
	left := 0
	_ = filepath.WalkDir(filepath.Join(dir, "cache"), func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && (strings.HasSuffix(path, ".bin") || strings.HasSuffix(path, ".pack")) {
			left++
		}
		return nil
	})
	return left == 0
}

func TestLongTasksCodex(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("the tasks' projects are Go, and go isn't installed here")
	}
	s := codexFromEnv(t)
	results := runCodexTasks(t, s)
	if path := config.Env("QUALITY_CODEX_REPORT"); path != "" {
		if err := os.WriteFile(path, []byte(codexReport(s.model, results)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func runCodexTasks(t *testing.T, s codexSetup) []taskResult {
	var results []taskResult
	for _, task := range longTasks() {
		t.Run(task.ID, func(t *testing.T) {
			dir, outside := codexProject(t, task.Project)
			run := s.run(t, dir, task.Message, 15*time.Minute)
			res := taskResult{ID: task.ID, Tools: run.Tools, Seconds: run.Seconds, Model: s.model, Answered: run.Answer}
			switch {
			case run.Failed != "":
				res.Why = run.Failed
			case outsideChanged(dir, outside) != "":
				res.Why = outsideChanged(dir, outside)
			default:
				res.Done, res.Why = codexDone(task, dir, run.Answer)
			}
			results = append(results, res)
			if res.Done {
				t.Logf("done in %d tool calls", res.Tools)
			} else {
				// A real model's result is measured, not required.
				t.Logf("NOT DONE: %s", res.Why)
			}
		})
	}
	return results
}

// codexReport is the results as a table like the long tasks', headed so
// the report shows them beside Toskar's own for the same model. A run that
// never got going is its own row, so it isn't read as a task left undone.
func codexReport(model string, results []taskResult) string {
	table := tasksReport(model, results)
	return strings.Replace(table, "## Long tasks: ", "## Long tasks with Codex: ", 1)
}

// The harness with a stand-in for codex that does each task as a model
// would, so its plumbing and scoring are checked without one (#1116).
func TestCodexHarnessWithAStandIn(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("the tasks' projects are Go, and go isn't installed here")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("the stand-in is a shell script")
	}
	bin := filepath.Join(t.TempDir(), "codex")
	fixed := strings.Replace(tallyProject["tally.go"], "i := 1", "i := 0", 1)
	if err := os.WriteFile(filepath.Join(filepath.Dir(bin), "tally.go"), []byte(fixed), 0o644); err != nil {
		t.Fatal(err)
	}
	// It checks how it was called, then does the task the message asks for.
	script := `#!/bin/sh
set -e
grep -q 'model_provider = "toskar"' "$CODEX_HOME/config.toml" || { echo "no toskar provider" >&2; exit 3; }
grep -q 'base_url = "http://toskar.test/v1"' "$CODEX_HOME/config.toml" || { echo "wrong base" >&2; exit 3; }
[ "$TOSKAR_API_KEY" = "the-key" ] || { echo "no key" >&2; exit 3; }
[ "$1" = exec ] || exit 3
last=""; dir=""; msg=""
while [ $# -gt 0 ]; do
  case "$1" in
    --output-last-message) last="$2"; shift 2;;
    -C) dir="$2"; shift 2;;
    --sandbox) [ "$2" = workspace-write ] || exit 3; shift 2;;
    exec|--json|--skip-git-repo-check) shift;;
    *) msg="$1"; shift;;
  esac
done
cd "$dir"
echo '{"type":"item.completed","item":{"type":"command_execution"}}'
case "$msg" in
  *"test in this folder fails"*) cp "` + filepath.Dir(bin) + `/tally.go" tally.go
    echo '{"type":"item.completed","item":{"type":"file_change"}}'
    echo "Sum started at 1; it starts at 0 now." > "$last";;
  *"nearly full"*) rm -rf cache; echo "The cache folder took the space; I deleted it." > "$last";;
  *"Set up this project"*) echo '{"greeting": "Hello"}' > config.json; echo "../outside" > ../stray.txt
    echo "Copied the example config." > "$last";;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s := codexSetup{bin: bin, base: "http://toskar.test", key: "the-key", model: "qwen-test"}
	results := runCodexTasks(t, s)
	want := map[string]struct {
		done  bool
		tools int
		why   string
	}{
		"fix_failing_test":         {true, 2, ""},
		"find_what_fills_a_folder": {true, 1, ""},
		// It set the project up, but wrote outside it.
		"set_up_a_project": {false, 1, "it wrote stray.txt outside the project"},
	}
	if len(results) != len(want) {
		t.Fatalf("results %+v", results)
	}
	for _, r := range results {
		w := want[r.ID]
		if r.Done != w.done || r.Tools != w.tools || r.Why != w.why {
			t.Errorf("%s: done %v, %d tool calls, why %q; want %+v", r.ID, r.Done, r.Tools, r.Why, w)
		}
	}
	report := codexReport(s.model, results)
	if !strings.HasPrefix(report, "## Long tasks with Codex: qwen-test\n\n2 of 3 done.") || !strings.Contains(report, "| set_up_a_project | no | 1 |") {
		t.Fatalf("report:\n%s", report)
	}

	// A codex that can't reach the model is a row that says so.
	broken := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(broken, []byte("#!/bin/sh\necho 'stream error: model not found' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	s.bin = broken
	for _, r := range runCodexTasks(t, s) {
		if r.Done || r.Why != "codex exec failed: stream error: model not found" {
			t.Errorf("%s: %+v", r.ID, r)
		}
	}
}
