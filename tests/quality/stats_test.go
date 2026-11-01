package quality

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// A real model's answers vary, so one run per case can swing the pass rate
// by a case either way (#834). These are the numbers the report gives for a
// set run more than once: the pass rate with a 95% interval, and, from
// three or more runs of a case, how often several tries would get it right.

// wilson is the 95% Wilson score interval for passes out of n, which stays
// inside 0 to 1 and behaves for small n, unlike passes/n ± 1.96σ.
func wilson(passes, n int) (lo, hi float64) {
	if n == 0 {
		return 0, 1
	}
	const z = 1.959963984540054
	p := float64(passes) / float64(n)
	nf := float64(n)
	denom := 1 + z*z/nf
	center := (p + z*z/(2*nf)) / denom
	half := z * math.Sqrt(p*(1-p)/nf+z*z/(4*nf*nf)) / denom
	return math.Max(0, center-half), math.Min(1, center+half)
}

// passAtK is the unbiased estimate that at least one of k tries passes,
// from c passes in n runs (Chen et al., 2021): 1 - C(n-c, k) / C(n, k).
func passAtK(n, c, k int) float64 {
	if n-c < k {
		return 1
	}
	prod := 1.0
	for i := n - c + 1; i <= n; i++ {
		prod *= 1 - float64(k)/float64(i)
	}
	return 1 - prod
}

// majorityOf3 is the chance that two or more of three tries, drawn from the
// n runs without replacement, passed: what answering three times and
// keeping the answer most agree on would score, as Deliberate's Auto mode
// does (#459), if agreeing meant passing.
func majorityOf3(n, c int) float64 {
	if n < 3 {
		return math.NaN()
	}
	choose := func(a, b int) float64 {
		if b < 0 || b > a {
			return 0
		}
		r := 1.0
		for i := 1; i <= b; i++ {
			r = r * float64(a-b+i) / float64(i)
		}
		return r
	}
	all := choose(n, 3)
	return (choose(c, 2)*choose(n-c, 1) + choose(c, 3)) / all
}

func TestWilson(t *testing.T) {
	for _, tc := range []struct {
		passes, n int
		lo, hi    float64
	}{
		{64, 70, 0.8253, 0.9601}, // the 1.11.2 release run
		{0, 10, 0, 0.2775},
		{10, 10, 0.7225, 1},
		{0, 0, 0, 1},
	} {
		lo, hi := wilson(tc.passes, tc.n)
		if math.Abs(lo-tc.lo) > 1e-3 || math.Abs(hi-tc.hi) > 1e-3 {
			t.Errorf("wilson(%d, %d) = %.4f–%.4f, want %.4f–%.4f", tc.passes, tc.n, lo, hi, tc.lo, tc.hi)
		}
	}
}

func TestPassAtK(t *testing.T) {
	for _, tc := range []struct {
		n, c, k int
		want    float64
	}{
		{3, 0, 1, 0},
		{3, 3, 1, 1},
		{3, 1, 1, 1.0 / 3},
		{4, 2, 3, 1}, // two failures can't fill three tries
		{4, 1, 3, 0.75},
		{10, 5, 3, 1 - 10.0/120},
	} {
		if got := passAtK(tc.n, tc.c, tc.k); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("passAtK(%d, %d, %d) = %v, want %v", tc.n, tc.c, tc.k, got, tc.want)
		}
	}
}

func TestMajorityOf3(t *testing.T) {
	for _, tc := range []struct {
		n, c int
		want float64
	}{
		{3, 3, 1},
		{3, 2, 1},
		{3, 1, 0},
		{4, 2, 0.5},
		{5, 3, 0.7},
	} {
		if got := majorityOf3(tc.n, tc.c); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("majorityOf3(%d, %d) = %v, want %v", tc.n, tc.c, got, tc.want)
		}
	}
	if !math.IsNaN(majorityOf3(2, 2)) {
		t.Error("two runs can't estimate three tries")
	}
}

// verdict is how a set's runs measure up (#834): against the bar, and
// against a baseline run when there is one. Pass rates vary from run to
// run, so a run fails only when its interval says it's below: the whole
// interval under the bar, or significantly worse than the baseline.
type verdict struct {
	fail bool
	// note says how it measured up, for the report and the log.
	note string
}

// significance is the one-sided p-value under which a drop from the
// baseline is a regression.
const significance = 0.05

// gate measures s against bar and, when base is set, against it. The stub's
// replies are scripted, so for it (bar 1) every case must pass.
func gate(s summary, bar float64, base *summary) verdict {
	var v verdict
	switch {
	case bar >= 1:
		if s.rate < 1 {
			return verdict{fail: true, note: "Every case must pass with the stub."}
		}
		return verdict{note: "Every case passed."}
	case s.hi < bar:
		v = verdict{fail: true, note: fmt.Sprintf("Below the %.0f%% bar: the whole interval is under it.", bar*100)}
	case s.rate < bar:
		v.note = fmt.Sprintf("Below the %.0f%% bar, but within run-to-run noise: the interval reaches it, so this isn't counted as a failure. More repeats would tell.", bar*100)
	default:
		v.note = fmt.Sprintf("At or above the %.0f%% bar.", bar*100)
	}
	if base == nil || base.runs == 0 {
		return v
	}
	p := dropP(base.passes, base.runs, s.passes, s.runs)
	if p < significance {
		v.fail = true
		v.note += fmt.Sprintf(" Worse than the baseline: %.0f%% then, %.0f%% now (one-sided p = %.3f).", base.rate*100, s.rate*100, p)
	} else {
		v.note += fmt.Sprintf(" No clear change from the baseline's %.0f%% (one-sided p = %.2f for a drop).", base.rate*100, p)
	}
	return v
}

// dropP is the one-sided p-value that the pass rate fell from passes1 of n1
// to passes2 of n2: a two-proportion z-test with the pooled rate.
func dropP(passes1, n1, passes2, n2 int) float64 {
	if n1 == 0 || n2 == 0 {
		return 1
	}
	p1, p2 := float64(passes1)/float64(n1), float64(passes2)/float64(n2)
	pool := float64(passes1+passes2) / float64(n1+n2)
	se := math.Sqrt(pool * (1 - pool) * (1/float64(n1) + 1/float64(n2)))
	if se == 0 {
		if p2 < p1 {
			return 0
		}
		return 1
	}
	z := (p2 - p1) / se
	return 0.5 * math.Erfc(-z/math.Sqrt2)
}

// A run fails only when its interval says so (#834).
func TestGate(t *testing.T) {
	sum := func(passes, runs int) summary {
		s := summary{passes: passes, runs: runs, rate: float64(passes) / float64(runs)}
		s.lo, s.hi = wilson(passes, runs)
		return s
	}
	base := sum(180, 200) // 90%
	for _, c := range []struct {
		name string
		run  summary
		base *summary
		fail bool
		note string
	}{
		{"above the bar", sum(190, 200), nil, false, "At or above"},
		{"just under, within noise", sum(54, 63), nil, false, "within run-to-run noise"},
		{"clearly under", sum(150, 200), nil, true, "whole interval"},
		{"the same as the baseline", sum(182, 200), &base, false, "No clear change"},
		{"a few worse, not clearly", sum(176, 200), &base, false, "No clear change"},
		{"clearly worse than the baseline", sum(165, 200), &base, true, "Worse than the baseline"},
		{"the stub misses one", sum(62, 63), nil, true, "Every case must pass"},
	} {
		bar := 0.9
		if c.name == "the stub misses one" {
			bar = 1
		}
		v := gate(c.run, bar, c.base)
		if v.fail != c.fail || !strings.Contains(v.note, c.note) {
			t.Errorf("%s: fail %v, %q", c.name, v.fail, v.note)
		}
	}
	if p := dropP(180, 200, 180, 200); math.Abs(p-0.5) > 1e-9 {
		t.Errorf("no change: p = %v", p)
	}
}
