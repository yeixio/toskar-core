package tools

import (
	"fmt"
	"strings"
	"sync"
)

// Policy constants.
const (
	PolicyDeny            = "deny"
	PolicyAsk             = "ask"
	PolicyAllowForSession = "allow-for-session"
	PolicyAllow           = "allow"
	// PolicyAskEachTime asks every time, even for a tool allowed for the
	// chat: a chat that read someone else's words gets it in place of
	// allow, so those words can't steer the tool on an earlier yes. It's
	// never a profile's setting.
	PolicyAskEachTime = "ask-each-time"
)

// PolicyEngine resolves tool permissions.
type PolicyEngine struct {
	mu sync.Mutex
	// sessionAllowed are the tools allowed for the rest of a chat, by the
	// chat's scope: never across chats or people.
	sessionAllowed map[string]map[string]struct{}
}

func NewPolicyEngine() *PolicyEngine {
	return &PolicyEngine{sessionAllowed: make(map[string]map[string]struct{})}
}

// Decide returns whether a tool may run and whether user approval is
// needed. scope is the chat it runs in, where an earlier "allow for
// session" counts; empty has none.
func (p *PolicyEngine) Decide(toolID, policy, scope string) (allowed bool, needsPrompt bool, err error) {
	policy = strings.ToLower(strings.TrimSpace(policy))
	if policy == "" {
		policy = PolicyAsk
	}
	switch policy {
	case PolicyDeny:
		return false, false, fmt.Errorf("tool %q denied by policy", toolID)
	case PolicyAllow:
		return true, false, nil
	case PolicyAllowForSession, PolicyAsk:
		if p.allowedIn(scope, toolID) {
			return true, false, nil
		}
		return false, true, nil
	case PolicyAskEachTime:
		return false, true, nil
	default:
		return false, false, fmt.Errorf("unknown policy %q", policy)
	}
}

func (p *PolicyEngine) allowedIn(scope, toolID string) bool {
	if scope == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.sessionAllowed[scope][toolID]
	return ok
}

// AllowSession allows toolID for the rest of the chat scope names, until
// Toskar restarts. Without a scope there's no chat to allow it in, so the
// next call asks again.
func (p *PolicyEngine) AllowSession(scope, toolID string) {
	if scope == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sessionAllowed[scope] == nil {
		p.sessionAllowed[scope] = make(map[string]struct{})
	}
	p.sessionAllowed[scope][toolID] = struct{}{}
}

// SessionScope is the chat a tool call's meta names, where "allow for
// session" applies: its conversation.
func SessionScope(meta map[string]any) string {
	if id, _ := meta["conversation_id"].(string); id != "" {
		return "conversation:" + id
	}
	return ""
}

// PolicyFor returns the policy string for a tool from profile policies.
func PolicyFor(toolID string, policies []ToolPolicy) string {
	for _, tp := range policies {
		if tp.ToolID == toolID {
			return tp.Policy
		}
	}
	return PolicyAsk
}

// ToolPolicy mirrors contracts.ToolPolicy locally.
type ToolPolicy struct {
	ToolID string `json:"tool_id"`
	Policy string `json:"policy"`
}
