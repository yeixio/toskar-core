package quality

import (
	"math"
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
