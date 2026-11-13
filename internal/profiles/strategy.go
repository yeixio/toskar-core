package profiles

import (
	"slices"
	"strings"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Strategies a profile can choose (spec §20). Empty is Auto.
const (
	StrategySingle  = "single"
	StrategyPlanned = "planned"
	StrategyTeam    = "team"
	// StrategySwarm has several agents work at once, each in a chat of its
	// own (#1135, docs/features/swarms.md). Auto never chooses it.
	StrategySwarm = "swarm"
)

// Model roles a profile can assign (spec §20). A role left empty uses the
// primary model.
const (
	// RolePrimary writes the answer.
	RolePrimary = "assistant"
	// RoleFast answers quick questions when the chat model is Auto.
	RoleFast = "fast"
	// RoleCoding answers coding requests when the chat model is Auto.
	RoleCoding = "coding"
	// RolePlanner splits a request into parts.
	RolePlanner = "planner"
	// RoleWorker works on one part of a plan.
	RoleWorker = "worker"
	// RoleReviewer checks the answer.
	RoleReviewer = "reviewer"
	// RoleDrafter writes one of the independent drafts Deliberate compares
	// (#459); slots are "drafter:1", "drafter:2", and so on.
	RoleDrafter = "drafter"
	// RoleJudge reconciles Deliberate's drafts into the answer.
	RoleJudge = "judge"
	// RoleAgent is one agent of a swarm (#1135); slots are "agent:1",
	// "agent:2", and so on.
	RoleAgent = "agent"
)

// legacyOrchestrator is the Team orchestrator id from before Team ran on the
// main pipeline. Profiles that name it are migrated by Normalize.
const legacyOrchestrator = "team"

// legacyRoles maps role names of older profiles onto the current roles.
var legacyRoles = map[string]string{
	"coordinator": RolePlanner,
	"researcher":  RolePrimary,
}

// Normalize moves a profile written for the old Team orchestrator onto the
// main pipeline with the Team strategy, keeping its roles and models (§37).
// Other profiles are returned unchanged.
func Normalize(p Profile) Profile {
	p.Topics = normalizeTopics(p.Topics)
	if p.OrchestratorID == legacyOrchestrator {
		p.OrchestratorID = "simple"
		if p.Orchestration.Strategy == "" {
			p.Orchestration.Strategy = StrategyTeam
		}
	}
	if len(p.Roles) > 0 {
		roles := make([]contracts.ModelRole, 0, len(p.Roles))
		seen := map[string]bool{}
		for _, r := range p.Roles {
			if to, ok := legacyRoles[r.Role]; ok {
				r.Role = to
			}
			if seen[r.Role] {
				continue
			}
			seen[r.Role] = true
			roles = append(roles, r)
		}
		p.Roles = roles
	}
	return p
}

// RoleModel is the model a profile assigns to role, or "" when the role
// uses the primary model. A worker slot such as "worker:2" uses the worker
// role's model.
func RoleModel(p Profile, role string) string {
	base := BaseRole(role)
	for _, r := range p.Roles {
		if r.Role == base && r.ModelID != "" {
			return r.ModelID
		}
	}
	return ""
}

// HasRole reports whether a profile assigns its own model to role.
func HasRole(p Profile, role string) bool { return RoleModel(p, role) != "" }

// BaseRole strips a worker slot's number: "worker:2" is "worker".
func BaseRole(role string) string {
	if i := strings.IndexByte(role, ':'); i > 0 {
		return role[:i]
	}
	return role
}

// normalizeTopics trims topic controls and drops empty lines; topics with
// nothing to stay on are none (#345).
func normalizeTopics(t *contracts.TopicPolicy) *contracts.TopicPolicy {
	if t == nil {
		return nil
	}
	out := *t
	out.StaysOn = strings.TrimSpace(out.StaysOn)
	out.OffTopicReply = strings.TrimSpace(out.OffTopicReply)
	clean := func(in []string) []string {
		var kept []string
		for _, s := range in {
			if s = strings.TrimSpace(s); s != "" {
				kept = append(kept, s)
			}
		}
		return kept
	}
	out.Examples, out.NeverDiscuss = clean(out.Examples), clean(out.NeverDiscuss)
	out.WebKeywords = clean(out.WebKeywords)
	var sites []string
	for _, site := range out.WebSites {
		if site = Site(site); site != "" && !slices.Contains(sites, site) {
			sites = append(sites, site)
		}
	}
	out.WebSites = sites
	if out.StaysOn == "" {
		return nil
	}
	return &out
}
