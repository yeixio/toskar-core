package terminal

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/turnopts"
)

func TestExecuteRequiresCommand(t *testing.T) {
	if _, err := New().Execute(context.Background(), map[string]any{"command": "  "}); err == nil {
		t.Fatal("blank command was accepted")
	}
}

func TestExecuteReportsOutputAndExit(t *testing.T) {
	tool := New()
	ok, err := tool.Execute(context.Background(), map[string]any{"command": "echo hi"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(ok["stdout"].(string)) != "hi" || ok["exit"] != 0 {
		t.Fatalf("result=%v", ok)
	}

	failed, err := tool.Execute(context.Background(), map[string]any{"command": "exit 3"})
	if err != nil {
		t.Fatal(err)
	}
	if failed["exit"] != 3 {
		t.Fatalf("result=%v", failed)
	}
	if !strings.Contains(failed["error"].(string), "exit status 3") {
		t.Fatalf("error=%v", failed["error"])
	}
}

func TestExecuteRunsInTheProject(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pwd := "pwd"
	if runtime.GOOS == "windows" {
		pwd = "(Get-Location).Path"
	}
	got, err := New().Execute(turnopts.WithProject(context.Background(), dir), map[string]any{"command": pwd})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(got["stdout"].(string)) != dir {
		t.Fatalf("ran in %v, want %s", got["stdout"], dir)
	}
}

// A model's command reaches PowerShell whole on Windows: quotes, pipes, and
// words outside English, in UTF-8.
func TestExecuteKeepsQuotesAndUnicode(t *testing.T) {
	got, err := New().Execute(context.Background(), map[string]any{"command": `echo "héllo wörld" | sort`})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(got["stdout"].(string)) != "héllo wörld" || got["exit"] != 0 {
		t.Fatalf("result=%v", got)
	}
}

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
}

// Output comes as the command writes it, not when it ends (#1017).
func TestOutputComesLive(t *testing.T) {
	skipOnWindows(t)
	var mu sync.Mutex
	var seen []string
	first := make(chan time.Time, 1)
	ctx := turnopts.WithCommandOutput(context.Background(), func(text string) {
		mu.Lock()
		defer mu.Unlock()
		if len(seen) == 0 {
			first <- time.Now()
		}
		seen = append(seen, text)
	})
	result, err := New().Execute(ctx, map[string]any{"command": "echo one; sleep 1; echo two"})
	if err != nil {
		t.Fatal(err)
	}
	ended := time.Now()
	if early := ended.Sub(<-first); early < 500*time.Millisecond {
		t.Errorf("first output only %v before the end", early)
	}
	if got := strings.Join(seen, ""); got != "one\ntwo\n" {
		t.Errorf("live output %q", got)
	}
	if result["stdout"] != "one\ntwo\n" {
		t.Errorf("kept output %q", result["stdout"])
	}
}

// Stopping a command stops what it started, and soon.
func TestStopStopsChildren(t *testing.T) {
	skipOnWindows(t)
	pidFile := filepath.Join(t.TempDir(), "pid")
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	started := time.Now()
	result, err := New().Execute(ctx, map[string]any{"command": "sleep 60 & echo $! > " + pidFile + "; wait"})
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(started); took > stopGrace {
		t.Errorf("took %v to stop", took)
	}
	if result["exit"] == 0 {
		t.Errorf("result %v", result)
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	for deadline := time.Now().Add(3 * time.Second); ; {
		if exec.Command("kill", "-0", strconv.Itoa(pid)).Run() != nil {
			break
		}
		if time.Now().After(deadline) {
			_ = exec.Command("kill", "-9", strconv.Itoa(pid)).Run()
			t.Fatal("the child kept running")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Something left running doesn't hold up the answer.
func TestBackgroundDoesNotHoldTheAnswer(t *testing.T) {
	skipOnWindows(t)
	before := outputGrace
	outputGrace = 200 * time.Millisecond
	t.Cleanup(func() { outputGrace = before })
	pidFile := filepath.Join(t.TempDir(), "pid")
	started := time.Now()
	result, err := New().Execute(context.Background(), map[string]any{"command": "sleep 5 & echo $! > " + pidFile + "; echo started"})
	if err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(pidFile); err == nil {
		_ = exec.Command("kill", strings.TrimSpace(string(raw))).Run()
	}
	if took := time.Since(started); took > 3*time.Second {
		t.Errorf("took %v", took)
	}
	if result["exit"] != 0 || result["error"] != nil || result["stdout"] != "started\n" {
		t.Errorf("result %v", result)
	}
}

func TestTimeLimitSaysSo(t *testing.T) {
	skipOnWindows(t)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	result, err := New().Execute(ctx, map[string]any{"command": "echo begun; sleep 60"})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := result["error"].(string); !strings.Contains(got, "time limit") {
		t.Errorf("error %q", got)
	}
	if result["stdout"] != "begun\n" {
		t.Errorf("what it wrote should be kept: %q", result["stdout"])
	}
}

// A long build keeps its start and end, not everything it wrote.
func TestOutputIsBounded(t *testing.T) {
	o := &output{}
	line := []byte(strings.Repeat("x", 1<<20))
	_, _ = o.Write([]byte("START"))
	for range 20 {
		_, _ = o.Write(line)
	}
	_, _ = o.Write([]byte("END"))
	got := o.String()
	if len(got) > maxOutput+maxOutput/2 {
		t.Errorf("kept %d bytes", len(got))
	}
	if !strings.HasPrefix(got, "START") || !strings.HasSuffix(got, "END") || !strings.Contains(got, "bytes left out") {
		t.Errorf("start, end, or note missing: %q...", got[:20])
	}
}

// A character split between writes isn't shown broken.
func TestLiveKeepsCharactersWhole(t *testing.T) {
	var got strings.Builder
	o := &output{live: func(text string) { got.WriteString(text) }}
	word := []byte("caf\u00e9\u65e5\u672c")
	for i := range word {
		_, _ = o.Write(word[i : i+1])
		o.send(false)
	}
	o.send(true)
	if got.String() != string(word) {
		t.Errorf("got %q", got.String())
	}
}
