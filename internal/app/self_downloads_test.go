package app

import (
	"context"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/egress"
	"github.com/yeixio/toskar-core/internal/gjallarhorn"
	"github.com/yeixio/toskar-core/internal/pyenv"
)

// What Toskar downloads by itself is in What left this computer, and a
// swap the person didn't start is told once in the app (#1209).
func TestSelfDownloadsRecordedAndTold(t *testing.T) {
	t.Setenv("TOSKAR_DISCOVERY_ENABLED", "false")
	a, err := New(Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Shutdown(context.Background()) })
	ctx := context.Background()

	a.runtimeUpgraded(ctx, "llamacpp")
	a.runtimeUpgraded(ctx, "llamacpp") // told once
	a.imagesUpdated(ctx, "sd-bin-Linux-x64.zip")
	a.pythonInstalled(ctx, pyenv.Spec{Name: "speech", Requirements: []string{"faster-whisper"}})

	recs, err := a.Egress.List(ctx, egress.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	var details []string
	for _, r := range recs {
		if r.Kind != egress.Download {
			t.Errorf("kind %q", r.Kind)
		}
		details = append(details, r.Detail)
	}
	all := strings.Join(details, "\n")
	for _, want := range []string{"llama.cpp: a faster build", "stable-diffusion.cpp", "Python " + pyenv.PythonVersion + " and packages for speech: faster-whisper"} {
		if !strings.Contains(all, want) {
			t.Errorf("records %q lack %q", all, want)
		}
	}

	list, _, err := a.Notifications.List(ctx, false, 0, gjallarhorn.CategorySystem)
	if err != nil {
		t.Fatal(err)
	}
	told := map[string]int{}
	for _, n := range list {
		if n.SourceType == "system" {
			told[n.SourceID]++
		}
	}
	if told["runtime-build:llamacpp"] != 1 || len(told) != 2 {
		t.Fatalf("told: %v", told)
	}
}
