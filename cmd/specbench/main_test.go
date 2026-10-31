package main

import (
	"encoding/json"
	"testing"
)

func plainAnswer(t *testing.T, raw string) answer {
	t.Helper()
	var toks []token
	if err := json.Unmarshal([]byte(raw), &toks); err != nil {
		t.Fatal(err)
	}
	a := answer{tokens: toks}
	for _, tk := range toks {
		a.Text += tk.Token
	}
	return a
}

// An answer that leaves the plain one where the plain run's top two were
// close is a near-tie; one that takes a word the plain run hardly
// considered is different.
func TestCompare(t *testing.T) {
	plain := plainAnswer(t, `[
		{"token":"Heat","top_logprobs":[{"token":"Heat","logprob":-0.1},{"token":"Warm","logprob":-2.5}]},
		{"token":" goes","top_logprobs":[{"token":" goes","logprob":-0.30},{"token":" moves","logprob":-0.32}]},
		{"token":" out","top_logprobs":[{"token":" out","logprob":-0.2},{"token":" away","logprob":-1.9}]}]`)
	if v := compare(plain, "Heat goes out"); !v.Same {
		t.Errorf("same: %+v", v)
	}
	if v := compare(plain, "Heat moves out"); !v.NearTie || v.Gap > 0.03 {
		t.Errorf("near-tie: %+v", v)
	}
	if v := compare(plain, "Warm air"); v.NearTie || v.Same {
		t.Errorf("second choice far behind: %+v", v)
	}
	if v := compare(plain, "Heat leaves"); v.NearTie || v.Same {
		t.Errorf("a word not in the top two: %+v", v)
	}
}
