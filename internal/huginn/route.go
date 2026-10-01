package huginn

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/yeixio/yggdrasil-core/pkg/contracts"
)

// AutoModelID is the model a chat asks for when it wants Yggdrasil to choose.
const AutoModelID = "auto"

// Shares of this computer's memory a model may need. A quick question gets a
// model that loads and answers fast; harder requests may use more.
const (
	quickShare = 0.40
	fullShare  = 0.60
)

// Choice is the model picked for a request and why, in plain language.
type Choice struct {
	Model  contracts.Model
	Reason string
}

func has(list []string, v string) bool { return slices.Contains(list, v) }

func toolRank(c contracts.ModelCapabilities) int {
	switch c.ToolCallSupport {
	case "native":
		return 3
	case "compatible":
		return 2
	case "limited":
		return 1
	case "unsupported":
		return 0
	}
	if c.ToolCalling {
		return 2
	}
	return 0
}

// general reports a model meant for conversation. Vision models and models
// that think out loud before answering are kept for when nothing else fits.
func general(m contracts.Model) bool {
	if has(m.Tags, "vision") {
		return false
	}
	if has(m.Tags, "reasoning") && !has(m.Tags, "general") {
		return false
	}
	if len(m.Purpose) > 0 && !has(m.Purpose, "general") && !has(m.Purpose, "assistant") && !has(m.Purpose, "research") {
		return false
	}
	return true
}

// suits reports whether a model is a good match for a kind of request.
func suits(k Kind, m contracts.Model) bool {
	switch k {
	case Coding:
		return m.Capabilities.Coding || has(m.Tags, "coding")
	case Current, Local:
		return toolRank(m.Capabilities) >= 2 && (general(m) || m.Capabilities.Coding)
	case Research:
		return general(m) && (has(m.Purpose, "research") || has(m.Tags, "reasoning") || has(m.Tags, "large") || has(m.Tags, "general"))
	default:
		return general(m)
	}
}

func fits(m contracts.Model, memTotal uint64, share float64) bool {
	if memTotal == 0 || m.MemoryNeeded == 0 {
		return true
	}
	return float64(m.MemoryNeeded) <= float64(memTotal)*share
}

// bigger prefers the model that needs more memory, a stand-in for quality.
func bigger(a, b contracts.Model) bool { return a.MemoryNeeded > b.MemoryNeeded }

func best(models []contracts.Model, keep func(contracts.Model) bool, better func(a, b contracts.Model) bool) (contracts.Model, bool) {
	var out contracts.Model
	found := false
	for _, m := range models {
		if !keep(m) {
			continue
		}
		if !found || better(m, out) {
			out, found = m, true
		}
	}
	return out, found
}

func running(m contracts.Model) bool { return m.Status == "running" }

// Choose picks an installed model for a request (spec §13). memTotal is this
// computer's memory in bytes, or 0 when unknown. It returns false when no
// model is installed.
func Choose(k Kind, installed []contracts.Model, memTotal uint64) (Choice, bool) {
	return ChooseFor(k, EffortAuto, installed, memTotal)
}

// ChooseFor is Choose with an effort: Fast keeps any request on a quick,
// already-loaded model where one suits; Thorough takes the largest suitable
// model that fits, even for a quick question.
func ChooseFor(k Kind, e Effort, installed []contracts.Model, memTotal uint64) (Choice, bool) {
	var models []contracts.Model
	for _, m := range installed {
		if m.Installed && !Supporting(m) {
			models = append(models, m)
		}
	}
	if len(models) == 0 {
		return Choice{}, false
	}
	quick := (k == Chat && e != EffortThorough) || e == EffortFast
	share := fullShare
	if quick {
		share = quickShare
	}
	pick := func(m contracts.Model) (Choice, bool) {
		reason := fmt.Sprintf("Auto chose %s for %s", Name(m), k.Describe())
		if e == EffortFast || e == EffortThorough {
			reason += fmt.Sprintf(" at %s effort", e.Label())
		}
		return Choice{Model: m, Reason: reason}, true
	}

	// A quick request goes to a suitable model that is already loaded, so it
	// is not slowed by loading another.
	if quick {
		if m, ok := best(models, func(m contracts.Model) bool { return running(m) && suits(k, m) && fits(m, memTotal, fullShare) }, bigger); ok {
			return pick(m)
		}
	}
	if m, ok := best(models, func(m contracts.Model) bool { return suits(k, m) && fits(m, memTotal, share) }, bigger); ok {
		return pick(m)
	}
	// Nothing ideal: any conversational model that fits, then anything that
	// fits, then the smallest installed model.
	if m, ok := best(models, func(m contracts.Model) bool { return general(m) && fits(m, memTotal, fullShare) }, bigger); ok {
		return pick(m)
	}
	if m, ok := best(models, func(m contracts.Model) bool { return fits(m, memTotal, fullShare) }, bigger); ok {
		return pick(m)
	}
	m, _ := best(models, func(contracts.Model) bool { return true }, func(a, b contracts.Model) bool { return a.MemoryNeeded < b.MemoryNeeded })
	return pick(m)
}

// Fallback picks the model to try after failed could not answer (spec §14,
// §26): a conversational model that needs no more memory than the one that
// failed, preferring the largest such model, else the smallest other model.
func Fallback(failed string, installed []contracts.Model, memTotal uint64) (contracts.Model, bool) {
	var failedModel contracts.Model
	var others []contracts.Model
	for _, m := range installed {
		if !m.Installed || Supporting(m) {
			continue
		}
		if m.ID == failed {
			failedModel = m
			continue
		}
		others = append(others, m)
	}
	if len(others) == 0 {
		return contracts.Model{}, false
	}
	limit := failedModel.MemoryNeeded
	noLarger := func(m contracts.Model) bool {
		return limit == 0 || m.MemoryNeeded == 0 || m.MemoryNeeded <= limit
	}
	if m, ok := best(others, func(m contracts.Model) bool { return general(m) && noLarger(m) && fits(m, memTotal, fullShare) }, bigger); ok {
		return m, true
	}
	return best(others, func(m contracts.Model) bool { return general(m) || m.Capabilities.Coding }, func(a, b contracts.Model) bool { return a.MemoryNeeded < b.MemoryNeeded })
}

// Smaller reports whether falling back from a to b is likely to give a less
// capable answer, so the user should be told.
func Smaller(a, b contracts.Model) bool {
	return a.MemoryNeeded > 0 && b.MemoryNeeded > 0 && b.MemoryNeeded < a.MemoryNeeded*3/4
}

// Name is how a model is shown to the user.
func Name(m contracts.Model) string {
	if m.DisplayName != "" {
		return m.DisplayName
	}
	return m.ID
}

// smallBelow is the size, in billions of parameters, under which a model is
// small: fast, but more likely to mix up facts and numbers.
const smallBelow = 4.0

// Billions reads a model's size from its Parameters label, such as "1B",
// "3.8B", or "500M". It returns false when the size is unknown.
func Billions(m contracts.Model) (float64, bool) {
	p := strings.ToUpper(strings.TrimSpace(m.Parameters))
	scale := 1.0
	switch {
	case strings.HasSuffix(p, "B"):
		p = strings.TrimSuffix(p, "B")
	case strings.HasSuffix(p, "M"):
		p, scale = strings.TrimSuffix(p, "M"), 0.001
	default:
		return 0, false
	}
	v, err := strconv.ParseFloat(p, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v * scale, true
}

// Small reports whether a model is small enough that its answers about
// files and data deserve a second look. Unknown sizes are not small.
func Small(m contracts.Model) bool {
	b, ok := Billions(m)
	return ok && b < smallBelow
}

// Larger returns the best installed model that is not small and fits this
// computer, to suggest instead of a small one.
func Larger(installed []contracts.Model, memTotal uint64) (contracts.Model, bool) {
	return best(installed, func(m contracts.Model) bool {
		return m.Installed && !Supporting(m) && !Small(m) && general(m) && fits(m, memTotal, fullShare)
	}, bigger)
}
