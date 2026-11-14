package quality

import "testing"

// An answer is matched as a person reads it (#1140): gpt-oss's narrow
// no-break spaces, in "March 9" and "9 AM – 6 PM", are spaces, and a
// non-breaking hyphen is a hyphen. Without this, four right answers of its
// first run failed.
func TestSeenSpaces(t *testing.T) {
	for answer, pattern := range map[string]string{
		"We decided to move the launch to **March 9**.": `(?i)march 9`,
		"- **Ana Ruiz** – Women’s winner, 2:51:07":      `Ana Ruiz`,
		"daily, 9 AM – 6 PM":                            `(?i)9\s*(a\.?m\.?)?\s*[-–]\s*6\s*p\.?m`,
		"1 200 euros, a well‑known price":               `1 200 euros, a well-known`,
	} {
		c := Case{What: "a case", Expect: Expect{AnswerMatches: pattern}}
		if failures := check("real", c, Result{Answer: answer}, nil); len(failures) > 0 {
			t.Errorf("%q against %q: %v", answer, pattern, failures)
		}
	}
	if got, want := scoreQuestionText("Ana Ruiz won."), 1; got != want {
		t.Errorf("a swarm's question scored %d of its fact, want %d", got, want)
	}
}

func scoreQuestionText(answer string) int {
	got, _ := scoreQuestion(swarmItem{Facts: []string{`Ana Ruiz`}}, answer)
	return got
}
