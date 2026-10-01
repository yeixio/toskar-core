package simple

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/yeixio/yggdrasil-core/internal/huginn"
	"github.com/yeixio/yggdrasil-core/internal/tools"
	"github.com/yeixio/yggdrasil-core/pkg/contracts"
	"github.com/yeixio/yggdrasil-core/pkg/pluginapi"
)

// Events for a plan's progress, shown as a checklist while it runs.
const (
	EventPlanCreated = "plan.created"
	EventPlanStep    = "plan.step"
)

// noteRunes caps one step's notes, so the final answer sees every step.
const noteRunes = 1500

const workerInstructions = "You are working on one part of a larger request. Write short notes for that part only: " +
	"the facts, numbers, names, and links that matter, from the reference material when there is some. " +
	"Do not answer the whole request, and do not write an introduction or a conclusion."

const planGuidance = "Yggdrasil worked through this request in parts; the notes from each part are in the reference material. " +
	"Use them to answer the whole request in one reply, and compare or combine them where the request asks. " +
	"Do not mention the notes or the parts."

// runPlan works through a request with several parts (spec §22–23). Web
// lookups for independent parts run at the same time; the model then writes
// notes for each part in turn, and later parts of a sequence see the notes
// before them. It returns the notes for the final answer.
func runPlan(ctx context.Context, env pluginapi.ExecutionEnvironment, profile contracts.AIProfile, role string, plan huginn.Plan, prompt, reference string, pages int) string {
	steps := plan.Steps
	// A step that asks for a file is done by the final answer, which writes
	// the file from everything gathered.
	if n := len(steps); n > 1 {
		if _, isFile := askedForFile(steps[n-1]); isFile {
			steps = steps[:n-1]
		}
	}
	if len(steps) == 0 {
		return ""
	}
	env.Emit(EventPlanCreated, map[string]any{"steps": plan.Steps, "parallel": plan.Parallel})
	web := plan.NeedsWeb && webAllowed(profile)

	looked := make([]string, len(steps))
	if web && plan.Parallel {
		var wg sync.WaitGroup
		for i, step := range steps {
			wg.Add(1)
			go func(i int, step string) {
				defer wg.Done()
				looked[i], _ = lookUp(ctx, env, profile, lookupQuery(step), step, pages)
			}(i, step)
		}
		wg.Wait()
	}

	var notes strings.Builder
	for i, step := range steps {
		if ctx.Err() != nil {
			break
		}
		env.Emit(EventPlanStep, map[string]any{"index": i, "step": step, "status": "running"})
		material := looked[i]
		if web && !plan.Parallel {
			material, _ = lookUp(ctx, env, profile, lookupQuery(step), step, pages)
		}
		ref := joinReference(reference, material)
		if !plan.Parallel && notes.Len() > 0 {
			ref = joinReference(ref, "Notes from earlier parts:\n"+notes.String())
		}
		ask := []pluginapi.ChatMessage{
			{Role: "system", Content: workerInstructions},
			{Role: "user", Content: withReference("The whole request: "+prompt+"\n\nYour part: "+step, ref)},
		}
		content, _, err := generateText(ctx, env, role, ask)
		if ctx.Err() != nil {
			// Stopped: keep the parts already finished, not this one.
			break
		}
		status := "done"
		note := strings.TrimSpace(tools.VisibleText(content))
		if err != nil || note == "" {
			status = "failed"
			note = "(no notes for this part)"
		}
		if utf8.RuneCountInString(note) > noteRunes {
			note = string([]rune(note)[:noteRunes]) + "…"
		}
		fmt.Fprintf(&notes, "[%d] %s\n%s\n\n", i+1, step, note)
		env.Emit(EventPlanStep, map[string]any{"index": i, "step": step, "status": status})
	}
	if notes.Len() == 0 {
		return ""
	}
	return "Notes from each part of the request:\n" + strings.TrimSpace(notes.String())
}
