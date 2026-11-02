package terminal

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

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
