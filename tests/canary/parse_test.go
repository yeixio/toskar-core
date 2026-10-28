package canary

import (
	"math"
	"os"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	b, err := os.ReadFile("testdata/base.txt")
	if err != nil {
		t.Fatal(err)
	}
	ppl, err := parseBase(string(b))
	if err != nil || ppl != 15.5741 {
		t.Fatalf("parseBase = %v, %v; want 15.5741", ppl, err)
	}

	// testdata/kld.txt is b11174 on the CPU against b11174 with Metal.
	b, err = os.ReadFile("testdata/kld.txt")
	if err != nil {
		t.Fatal(err)
	}
	s, err := parseKLD(string(b))
	if err != nil {
		t.Fatal(err)
	}
	want := Stats{PPL: 15.607327, LnRatio: 0.002987, MeanKLD: 0.002451, KLD99: 0.017440, MaxKLD: 0.066882, SameTop: 0.97492, RMSDelta: 1.192}
	if math.Abs(s.SameTop-want.SameTop) > 1e-9 {
		t.Fatalf("SameTop = %v, want %v", s.SameTop, want.SameTop)
	}
	s.SameTop = want.SameTop
	if s != want {
		t.Fatalf("parseKLD = %+v, want %+v", s, want)
	}

	if _, err := parseKLD("llama-perplexity: error loading model"); err == nil {
		t.Fatal("parseKLD read output with no statistics")
	}
}

func TestCheck(t *testing.T) {
	g, err := readGoldens("goldens.json")
	if err != nil {
		t.Fatal(err)
	}
	if g.GoodTag == "" || g.Context <= 0 || g.Model.SHA256 == "" {
		t.Fatalf("goldens.json is missing the good tag, context or model: %+v", g)
	}
	cpu := Stats{MeanKLD: 0.002451, SameTop: 0.97492, LnRatio: 0.002987}
	if miss := cpu.check(g.CPU); miss != "" {
		t.Fatalf("Metal against the CPU, as measured, misses the CPU tolerance: %s", miss)
	}
	same := Stats{MeanKLD: 0.000002, SameTop: 0.99947, LnRatio: 0.000866}
	if miss := same.check(g.Release); miss != "" {
		t.Fatalf("a matching release misses the release tolerance: %s", miss)
	}
	broken := Stats{MeanKLD: 0.2, SameTop: 0.8, LnRatio: math.Log(1.1)}
	miss := broken.check(g.Release)
	for _, want := range []string{"mean KL divergence", "top token", "perplexity changed by +10.000%"} {
		if !strings.Contains(miss, want) {
			t.Errorf("check(broken) = %q, want it to say %q", miss, want)
		}
	}
}

func TestTagOf(t *testing.T) {
	for dir, want := range map[string]string{
		"/cache/llama.cpp-b11946-macos-arm64/llama-b11946": "b11946",
		"":           "",
		"/opt/llama": "llama",
	} {
		if got := tagOf(dir); got != want {
			t.Errorf("tagOf(%q) = %q, want %q", dir, got, want)
		}
	}
}
