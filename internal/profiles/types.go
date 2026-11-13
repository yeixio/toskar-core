package profiles

import (
	"fmt"
	"slices"
	"strings"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Profile is an alias for the public contract.
type Profile = contracts.AIProfile

// Validate checks basic profile invariants.
func Validate(p Profile) error {
	if p.Name == "" {
		return ErrInvalidProfile("name is required")
	}
	if p.OrchestratorID == "" {
		return ErrInvalidProfile("orchestrator_id is required")
	}
	for _, r := range p.Roles {
		if r.Role == "" {
			return ErrInvalidProfile("role name is required")
		}
		if r.Required && r.ModelID == "" {
			return ErrInvalidProfile("required role " + r.Role + " missing model_id")
		}
	}
	if err := ValidateNodePolicy(p.NodePolicy); err != nil {
		return err
	}
	if err := ValidateTopics(p.Topics); err != nil {
		return err
	}
	return ValidateOrchestration(p.Orchestration)
}

// ValidateTopics checks a profile's topic controls (#345).
func ValidateTopics(t *contracts.TopicPolicy) error {
	if t == nil {
		return nil
	}
	long := func(s string, most int) bool { return len([]rune(s)) > most }
	switch {
	case strings.TrimSpace(t.StaysOn) == "":
		return ErrInvalidProfile("topics: say what the assistant stays on")
	case long(t.StaysOn, 1000):
		return ErrInvalidProfile("topics: what it stays on can be 1000 characters")
	case len(t.Examples) > 20 || len(t.NeverDiscuss) > 20:
		return ErrInvalidProfile("topics: up to 20 examples and 20 subjects it never discusses")
	case long(t.OffTopicReply, 500):
		return ErrInvalidProfile("topics: the reply to anything else can be 500 characters")
	}
	for _, s := range append(append([]string(nil), t.Examples...), t.NeverDiscuss...) {
		if long(s, 200) {
			return ErrInvalidProfile("topics: each example and subject can be 200 characters")
		}
	}
	if len(t.WebSites) > 20 || len(t.WebKeywords) > 10 {
		return ErrInvalidProfile("topics: up to 20 sites and 10 search words")
	}
	for _, site := range t.WebSites {
		if !validSite(site) {
			return ErrInvalidProfile("topics: " + site + " isn't a site, such as example.com")
		}
	}
	for _, w := range t.WebKeywords {
		if long(w, 50) {
			return ErrInvalidProfile("topics: each search word can be 50 characters")
		}
	}
	switch t.Strictness {
	case "", contracts.TopicsGuide, contracts.TopicsEnforce:
	default:
		return ErrInvalidProfile("topics: strictness is guide or enforce")
	}
	return nil
}

// ValidateNodePolicy checks a profile's placement rules.
func ValidateNodePolicy(n contracts.NodePolicy) error {
	switch n.Mode {
	case "", "automatic", "prefer_local", "manual":
	default:
		return ErrInvalidProfile("node_policy.mode must be automatic, prefer_local, or manual")
	}
	if n.Remote != "" && n.Remote != "off" {
		return ErrInvalidProfile("node_policy.remote must be off or empty")
	}
	for _, id := range n.PreferredNodes {
		if slices.Contains(n.DeniedNodes, id) {
			return ErrInvalidProfile("a computer cannot be both preferred and denied")
		}
	}
	return nil
}

// ValidateOrchestration checks a profile's advanced controls (§40).
func ValidateOrchestration(o contracts.OrchestrationPolicy) error {
	choice := func(field, v string, allowed ...string) error {
		if v != "" && !slices.Contains(allowed, v) {
			return ErrInvalidProfile(fmt.Sprintf("orchestration.%s must be one of %v", field, allowed))
		}
		return nil
	}
	between := func(field string, v, lo, hi int) error {
		if v != 0 && (v < lo || v > hi) {
			return ErrInvalidProfile(fmt.Sprintf("orchestration.%s must be from %d to %d", field, lo, hi))
		}
		return nil
	}
	for _, err := range []error{
		choice("effort", o.Effort, "fast", "balanced", "thorough"),
		choice("strategy", o.Strategy, StrategySingle, StrategyPlanned, StrategyTeam, StrategySwarm),
		choice("planning", o.Planning, "on", "off", "always"),
		choice("parallel", o.Parallel, "on", "off"),
		choice("verification", o.Verification, "off", "check", "correct", "thorough"),
		choice("memory", o.Memory, "off"),
		choice("fallback", o.Fallback, "off"),
		choice("deliberate", o.Deliberate, "never", "always", "auto"),
		choice("media_quality", o.MediaQuality, "fast", "balanced", "best"),
		between("max_workers", o.MaxWorkers, 2, 8),
		between("max_tool_calls", o.MaxToolCalls, 1, 50),
		between("retries", o.Retries, 1, 3),
		between("timeout_seconds", o.TimeoutSeconds, 10, 3600),
	} {
		if err != nil {
			return err
		}
	}
	if len(o.FallbackModels) > 8 {
		return ErrInvalidProfile("orchestration.fallback_models may list up to 8 models")
	}
	for _, id := range o.FallbackModels {
		if id == "" {
			return ErrInvalidProfile("orchestration.fallback_models cannot contain an empty model id")
		}
	}
	if o.ContextShare != 0 && (o.ContextShare < 0.1 || o.ContextShare > 0.9) {
		return ErrInvalidProfile("orchestration.context_share must be from 0.1 to 0.9")
	}
	return ValidateSwarm(o.Swarm)
}

// Swarm limits (#1135, docs/features/swarms.md#budgets).
const (
	SwarmMinAgents  = 2
	SwarmMaxAgents  = 8
	SwarmMinTokens  = 10_000
	SwarmMaxTokens  = 4_000_000
	SwarmMinMinutes = 1
	SwarmMaxMinutes = 240
)

// Swarm modes (#1135).
const (
	SwarmSplit = "split"
	SwarmRace  = "race"
)

// ValidateSwarm checks a profile's or a turn's swarm controls (#1135).
func ValidateSwarm(s *contracts.SwarmPolicy) error {
	if s == nil {
		return nil
	}
	if s.Mode != "" && s.Mode != SwarmSplit && s.Mode != SwarmRace {
		return ErrInvalidProfile(fmt.Sprintf("orchestration.swarm.mode must be one of %v", []string{SwarmSplit, SwarmRace}))
	}
	for _, c := range []struct {
		field  string
		v      int
		lo, hi int
	}{
		{"agents", s.Agents, SwarmMinAgents, SwarmMaxAgents},
		{"tokens", s.Tokens, SwarmMinTokens, SwarmMaxTokens},
		{"minutes", s.Minutes, SwarmMinMinutes, SwarmMaxMinutes},
	} {
		if c.v != 0 && (c.v < c.lo || c.v > c.hi) {
			return ErrInvalidProfile(fmt.Sprintf("orchestration.swarm.%s must be from %d to %d", c.field, c.lo, c.hi))
		}
	}
	if len(s.Models) > SwarmMaxAgents {
		return ErrInvalidProfile(fmt.Sprintf("orchestration.swarm.models may list up to %d models", SwarmMaxAgents))
	}
	for _, id := range s.Models {
		if strings.TrimSpace(id) == "" {
			return ErrInvalidProfile("orchestration.swarm.models cannot contain an empty model id")
		}
	}
	return nil
}

// ErrInvalidProfile indicates profile validation failure.
type ErrInvalidProfile string

func (e ErrInvalidProfile) Error() string { return string(e) }

// validSite reports a site name, such as example.com: letters, digits,
// dashes, and at least one dot.
func validSite(site string) bool {
	if len(site) > 253 || !strings.Contains(site, ".") || strings.HasPrefix(site, ".") || strings.HasSuffix(site, ".") {
		return false
	}
	for _, r := range site {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '.':
		default:
			return false
		}
	}
	return !strings.Contains(site, "..")
}

// Site is a site as typed, such as "https://www.Example.com/shop", made a
// site name: "example.com".
func Site(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, "@"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.Index(s, ":"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimPrefix(strings.TrimSuffix(s, "."), "www.")
}
