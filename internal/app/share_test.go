package app

import (
	"context"
	"strings"
	"testing"
	"time"

	modelhealth "github.com/yeixio/yggdrasil-core/internal/models/health"
	"github.com/yeixio/yggdrasil-core/internal/share"
)

func TestChatExplainsTrainingOnThisComputer(t *testing.T) {
	a := &App{Share: share.New(0)}
	if _, ok := a.trainingNow(); ok {
		t.Fatal("training reported with none running")
	}
	oom := modelhealth.Encode(modelhealth.Failure{LikelyMemoryPressure: true, ModelID: "big"})
	if got := a.explainWhileTraining(oom); got != oom {
		t.Fatal("error rewritten with no training running")
	}

	w, err := a.Share.Enter(context.Background(), share.Training, "Tire shop", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Done()
	busy, ok := a.trainingNow()
	if !ok || busy != `Training "Tire shop" is using this computer` {
		t.Fatalf("busy = %q", busy)
	}
	w.SetRemaining(12*time.Minute + 10*time.Second)
	if busy, _ = a.trainingNow(); !strings.HasSuffix(busy, "(about 12 minutes left)") {
		t.Fatalf("busy = %q", busy)
	}

	// A memory failure keeps its structure but says why and when to retry.
	got := a.explainWhileTraining(oom)
	f, isHealth := modelhealth.Parse(got)
	if !isHealth || f.ModelID != "big" || !strings.HasPrefix(f.Message, `Training "Tire shop" is using this computer (about 12 minutes left), so there was not enough memory`) {
		t.Fatalf("explained = %q", got)
	}
	if got := a.explainWhileTraining("llama-server: failed to allocate buffer"); !strings.Contains(got, "Try again when training finishes") {
		t.Fatalf("plain error = %q", got)
	}
	// Other failures are left alone.
	if got := a.explainWhileTraining("connection refused"); got != "connection refused" {
		t.Fatalf("unrelated error = %q", got)
	}
}

func TestAboutLeft(t *testing.T) {
	g := share.New(0)
	w, _ := g.Enter(context.Background(), share.Training, "", nil)
	defer w.Done()
	for d, want := range map[time.Duration]string{
		20 * time.Second: " (less than a minute left)",
		70 * time.Second: " (about 1 minute left)",
		3 * time.Hour:    " (about 3 hours left)",
	} {
		w.SetRemaining(d)
		if got := aboutLeft(w); got != want {
			t.Errorf("%v: %q, want %q", d, got, want)
		}
	}
}
