package training

import (
	"context"
	"os"
	"strings"
	"testing"
)

// Routing sees a deployed AI with its training questions, and never one
// whose adapter is missing from this computer (§61).
func TestDeployedNeedsItsAdapterHere(t *testing.T) {
	h := newHarness(t, "ok")
	ctx := context.Background()
	ai, _ := h.svc.CreateAI(ctx, CreateInput{Name: "Tire Bot", Goal: "Answer tire questions", BaseModelID: "small-q4"})
	if _, err := h.svc.AddMaterial(ctx, ai.ID, MaterialInput{Filename: "c.jsonl", Text: tireExamples(12)}); err != nil {
		t.Fatal(err)
	}
	if list, _ := h.svc.Deployed(ctx); len(list) != 0 {
		t.Fatalf("undeployed AI listed: %+v", list)
	}
	job, _ := h.svc.StartTraining(ctx, ai.ID, "")
	waitJob(t, h, job.ID)
	if _, err := h.svc.Deploy(ctx, ai.ID, 1); err != nil {
		t.Fatal(err)
	}
	list, err := h.svc.Deployed(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("deployed = %+v, %v", list, err)
	}
	d := list[0]
	if d.ModelID != "sai:tire-bot" || d.BaseModelID != "small-q4" || len(d.Questions) != 12 || !strings.Contains(d.Questions[0], "about tires") {
		t.Fatalf("deployed = %+v", d)
	}

	rev, err := h.svc.d.Repo.GetRevision(ctx, ai.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(rev.adapterPath); err != nil {
		t.Fatal(err)
	}
	if list, _ := h.svc.Deployed(ctx); len(list) != 0 {
		t.Fatal("AI without its adapter listed for routing")
	}
	if _, err := h.svc.Resolve(ctx, "sai:tire-bot"); err == nil || !strings.Contains(err.Error(), "adapter is missing") {
		t.Fatalf("resolve without adapter: %v", err)
	}
}
