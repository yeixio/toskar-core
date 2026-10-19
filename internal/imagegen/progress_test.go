package imagegen

import (
	"context"
	"os"
	"testing"
)

// sd-cli's real output for FLUX.2 [klein] 4B, four steps, on a Mac
// (testdata/sdcli-flux2-klein.txt), read in uneven pieces as a pipe gives
// it: loading, then each step with an estimate, then finishing (#706).
func TestProgressFromSDCLIOutput(t *testing.T) {
	raw, err := os.ReadFile("testdata/sdcli-flux2-klein.txt")
	if err != nil {
		t.Fatal(err)
	}
	var got []Progress
	w := newProgressWriter(func(p Progress) { got = append(got, p) })
	for i := 0; i < len(raw); i += 97 {
		end := min(i+97, len(raw))
		if _, err := w.Write(raw[i:end]); err != nil {
			t.Fatal(err)
		}
	}
	want := []Progress{
		{Stage: "loading"},
		{Stage: "drawing", Step: 1, Steps: 4},
		{Stage: "drawing", Step: 2, Steps: 4, SecondsLeft: 3},
		{Stage: "drawing", Step: 3, Steps: 4, SecondsLeft: 2},
		{Stage: "drawing", Step: 4, Steps: 4},
		{Stage: "finishing"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d reports %+v, want %+v", len(got), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("report %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestProgressGoesWithTheContext(t *testing.T) {
	if progressFrom(context.Background()) != nil {
		t.Fatal("a context without a report has one")
	}
	called := false
	ctx := WithProgress(context.Background(), func(Progress) { called = true })
	progressFrom(ctx)(Progress{})
	if !called {
		t.Fatal("report not called")
	}
}
