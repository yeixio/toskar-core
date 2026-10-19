package imagegen

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Progress is how far a picture or clip has got (#706), read from sd-cli's
// output as it runs.
type Progress struct {
	// Stage is loading (the model into memory), drawing (the steps), or
	// finishing (turning the result into a picture or clip).
	Stage string
	// Step and Steps are the drawing step and how many there are.
	Step, Steps int
	// SecondsLeft is an estimate from the last step's time, 0 when not
	// known yet, as during the first step.
	SecondsLeft int
}

type progressKey struct{}

// WithProgress has a picture or clip made with ctx report its progress to
// report, as it changes.
func WithProgress(ctx context.Context, report func(Progress)) context.Context {
	return context.WithValue(ctx, progressKey{}, report)
}

func progressFrom(ctx context.Context) func(Progress) {
	report, _ := ctx.Value(progressKey{}).(func(Progress))
	return report
}

// sd-cli draws a bar per step, "|====>   | 2/4 - 1.51s/it", and loads
// weights with "|#####   | 28/298 - 641.64MB/s".
var (
	stepRe    = regexp.MustCompile(`\|[=>\s]*\|\s*(\d+)/(\d+)\s*-\s*([\d.]+)\s*(s/it|it/s)`)
	loadingRe = regexp.MustCompile(`(?i)loading (diffusion model|llm|vae|t5xxl|clip|model) from|\|#+\s*\|?.*MB/s`)
	decodeRe  = regexp.MustCompile(`(?i)decoding \d+ latents?|decode_first_stage`)
)

// progressWriter reads sd-cli's output and reports each change of stage
// or step.
type progressWriter struct {
	mu      sync.Mutex
	report  func(Progress)
	partial string
	last    Progress
}

func newProgressWriter(report func(Progress)) *progressWriter {
	return &progressWriter{report: report}
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.partial += string(p)
	// Bars redraw with carriage returns; lines end with newlines.
	for {
		i := strings.IndexAny(w.partial, "\r\n")
		if i < 0 {
			break
		}
		w.line(w.partial[:i])
		w.partial = w.partial[i+1:]
	}
	if len(w.partial) > 4096 {
		w.partial = w.partial[len(w.partial)-1024:]
	}
	return len(p), nil
}

func (w *progressWriter) line(s string) {
	var next Progress
	switch {
	case stepRe.MatchString(s):
		m := stepRe.FindStringSubmatch(s)
		step, _ := strconv.Atoi(m[1])
		steps, _ := strconv.Atoi(m[2])
		rate, _ := strconv.ParseFloat(m[3], 64)
		perStep := rate
		if m[4] == "it/s" && rate > 0 {
			perStep = 1 / rate
		}
		next = Progress{Stage: "drawing", Step: step, Steps: steps}
		// The first step's time includes warming up (11 s, then 1.5 s a
		// step, on a Mac), so the estimate starts with the second.
		if step > 1 && step < steps && perStep > 0 {
			next.SecondsLeft = int(float64(steps-step)*perStep + 0.5)
			if next.SecondsLeft < 1 {
				next.SecondsLeft = 1
			}
		}
	case decodeRe.MatchString(s):
		// Finishing comes after drawing; before it, it's the model's setup.
		if w.last.Stage != "drawing" {
			return
		}
		next = Progress{Stage: "finishing"}
	case loadingRe.MatchString(s):
		// Once drawing has started, a later load (the VAE) is part of it.
		if w.last.Stage == "drawing" || w.last.Stage == "finishing" {
			return
		}
		next = Progress{Stage: "loading"}
	default:
		return
	}
	if next.Stage == w.last.Stage && next.Step == w.last.Step {
		return
	}
	w.last = next
	w.report(next)
}
