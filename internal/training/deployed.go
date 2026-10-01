package training

import (
	"context"
	"os"
	"strings"
)

// maxQuestions caps the training questions kept per AI for routing.
const maxQuestions = 300

// Deployed is a specialized AI ready to answer on this computer.
type Deployed struct {
	AI          SpecializedAI
	ModelID     string
	BaseModelID string
	// Questions are the user turns of its training examples, so routing
	// can tell what it was trained for.
	Questions []string
}

// Deployed lists the specialized AIs that can answer here: deployed, with
// their adapter on this computer (spec §61). An AI whose adapter is missing
// is left out, so Auto never routes to it.
func (s *Service) Deployed(ctx context.Context) ([]Deployed, error) {
	ais, err := s.d.Repo.ListAIs(ctx)
	if err != nil {
		return nil, err
	}
	var out []Deployed
	for _, ai := range ais {
		if ai.DeployedRevision == 0 {
			continue
		}
		rev, err := s.d.Repo.GetRevision(ctx, ai.ID, ai.DeployedRevision)
		if err != nil {
			continue
		}
		if _, err := os.Stat(rev.adapterPath); err != nil {
			continue
		}
		out = append(out, Deployed{AI: ai, ModelID: ModelPrefix + ai.Slug, BaseModelID: rev.BaseModelID, Questions: s.trainingQuestions(ctx, ai.ID, rev.AdapterID())})
	}
	return out, nil
}

func (s *Service) trainingQuestions(ctx context.Context, aiID, key string) []string {
	s.mu.Lock()
	cached, ok := s.questions[key]
	s.mu.Unlock()
	if ok {
		return cached
	}
	examples, err := s.d.Repo.ListExamples(ctx, aiID)
	if err != nil {
		return nil
	}
	var out []string
	for _, ex := range examples {
		if ex.Excluded {
			continue
		}
		for _, m := range ex.Messages {
			if m.Role == "user" && strings.TrimSpace(m.Content) != "" {
				out = append(out, m.Content)
				break
			}
		}
		if len(out) >= maxQuestions {
			break
		}
	}
	s.mu.Lock()
	s.questions[key] = out
	s.mu.Unlock()
	return out
}
