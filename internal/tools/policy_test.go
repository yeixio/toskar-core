package tools

import "testing"

func TestPolicyDeny(t *testing.T) {
	p := NewPolicyEngine()
	allowed, prompt, err := p.Decide("terminal", PolicyDeny, "")
	if err == nil || allowed || prompt {
		t.Fatalf("deny: allowed=%v prompt=%v err=%v", allowed, prompt, err)
	}
}

func TestPolicyAllow(t *testing.T) {
	p := NewPolicyEngine()
	allowed, prompt, err := p.Decide("git.status", PolicyAllow, "")
	if err != nil || !allowed || prompt {
		t.Fatalf("allow: allowed=%v prompt=%v err=%v", allowed, prompt, err)
	}
}

func TestPolicyAllowForSession(t *testing.T) {
	p := NewPolicyEngine()
	allowed, prompt, _ := p.Decide("filesystem.write", PolicyAllowForSession, "conversation:a")
	if allowed || !prompt {
		t.Fatal("expected prompt first")
	}
	p.AllowSession("conversation:a", "filesystem.write")
	allowed, prompt, err := p.Decide("filesystem.write", PolicyAllowForSession, "conversation:a")
	if err != nil || !allowed || prompt {
		t.Fatalf("session: allowed=%v prompt=%v err=%v", allowed, prompt, err)
	}
}

func TestPolicyAsk(t *testing.T) {
	p := NewPolicyEngine()
	allowed, prompt, err := p.Decide("terminal", PolicyAsk, "")
	if err != nil || allowed || !prompt {
		t.Fatalf("ask: allowed=%v prompt=%v err=%v", allowed, prompt, err)
	}
}

// "Allow for session" holds in the chat it was given in, under Ask too, and
// nowhere else: not in another chat, and not without one (#959).
func TestPolicyAllowForSessionIsPerChat(t *testing.T) {
	p := NewPolicyEngine()
	p.AllowSession("conversation:a", "terminal")
	p.AllowSession("", "terminal")
	for _, c := range []struct {
		policy, scope string
		allowed       bool
	}{
		{PolicyAsk, "conversation:a", true},
		{PolicyAllowForSession, "conversation:a", true},
		{PolicyAsk, "conversation:b", false},
		{PolicyAsk, "", false},
		// After untrusted content, it asks each time all the same.
		{PolicyAskEachTime, "conversation:a", false},
	} {
		allowed, prompt, err := p.Decide("terminal", c.policy, c.scope)
		if err != nil || allowed != c.allowed || prompt == c.allowed {
			t.Errorf("%s in %q: allowed=%v prompt=%v err=%v", c.policy, c.scope, allowed, prompt, err)
		}
	}
	if _, _, err := p.Decide("terminal", PolicyDeny, "conversation:a"); err == nil {
		t.Error("deny must stay deny")
	}
}
