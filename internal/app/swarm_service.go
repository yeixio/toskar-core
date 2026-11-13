package app

import (
	"context"

	"github.com/yeixio/toskar-core/internal/api"
	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/swarm"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// swarmService is swarms for the API (#1135): the person's own.
type swarmService struct{ app *App }

var _ api.SwarmService = (*swarmService)(nil)

// Start runs a swarm as a turn in the person's chat, or a new one, in the
// background. The record is made first, so the caller has its id; its agents
// fill in once the coordinator has planned them.
func (s *swarmService) Start(ctx context.Context, in swarm.Start) (contracts.Swarm, error) {
	a := s.app
	var conv contracts.Conversation
	var err error
	if in.ConversationID != "" {
		if conv, err = a.Conversations.Get(ctx, in.ConversationID); err != nil {
			return contracts.Swarm{}, api.ErrConversationNotFound
		}
	} else {
		if conv, err = a.Conversations.Create(ctx, swarm.JobName(in.Prompt), "", ""); err != nil {
			return contracts.Swarm{}, err
		}
	}
	if in.ProjectID != "" && conv.ProjectID == "" {
		if err := a.Conversations.SetProjectID(ctx, conv.ID, in.ProjectID); err != nil {
			return contracts.Swarm{}, err
		}
		conv.ProjectID = in.ProjectID
	}
	profileID := conv.ProfileID
	if profileID == "" {
		profileID = a.defaultProfileID(ctx)
	}
	var policy *contracts.SwarmPolicy
	maxWorkers := 0
	if p, err := a.Profiles.Get(ctx, profileID); err == nil {
		policy, maxWorkers = p.Orchestration.Swarm, p.Orchestration.MaxWorkers
	}
	mode := swarm.ModeFor(in.Mode, policy, in.Budget)
	budget := swarm.BudgetFor(policy, in.Budget, maxWorkers, false)
	rec, err := a.Swarms.Create(ctx, conv.ProjectID, conv.ID, mode, in.Prompt, budget.Contract())
	if err != nil {
		return contracts.Swarm{}, err
	}
	if err := a.Conversations.SetSwarm(ctx, conv.ID, rec.ID, ""); err != nil {
		return contracts.Swarm{}, err
	}
	// The turn outlives the request, as the person's.
	turn := auth.WithPrincipal(context.Background(), auth.PrincipalFrom(ctx))
	turn = swarm.WithID(swarm.WithMode(swarm.WithPolicy(turn, in.Budget), mode), rec.ID)
	go func() {
		ch, err := a.RunChat(turn, "", conv.ID, in.Prompt, true, "", "")
		if err == nil {
			for range ch {
			}
		}
		// A turn that answered without its agents, such as one Toskar
		// answered itself, leaves the record finished.
		if got, gerr := a.Swarms.Get(turn, rec.ID); gerr == nil && (got.Status == swarm.StatusRunning || got.Status == swarm.StatusMerging) {
			got.Status = swarm.StatusFailed
			if err == nil {
				got.Status = swarm.StatusDone
			}
			_ = a.Swarms.Save(turn, got)
		}
	}()
	return rec, nil
}

// Get is one of the person's swarms, with what its agents are waiting on.
func (s *swarmService) Get(ctx context.Context, id string) (contracts.Swarm, error) {
	rec, err := s.app.Swarms.Get(ctx, id)
	if err != nil {
		return contracts.Swarm{}, err
	}
	if s.app.Tools != nil {
		if waiting := s.app.Tools.WaitingInSwarm(id, auth.PersonID(ctx)); len(waiting) > 0 {
			rec.Waiting = waiting
		}
	}
	return rec, nil
}

func (s *swarmService) List(ctx context.Context, projectID string) ([]contracts.Swarm, error) {
	return s.app.Swarms.List(ctx, projectID)
}

// Stop stops a swarm's coordinator turn, which stops every agent, and each
// agent's own chat in case one goes on.
func (s *swarmService) Stop(ctx context.Context, id string) (bool, error) {
	rec, err := s.app.Swarms.Get(ctx, id)
	if err != nil {
		return false, err
	}
	stopped := s.app.StopChatFor(ctx, rec.ConversationID)
	for _, ag := range rec.Agents {
		if s.app.StopChatFor(ctx, ag.ConversationID) {
			stopped = true
		}
	}
	return stopped, nil
}

func (s *swarmService) Delete(ctx context.Context, id string) error {
	return s.app.Swarms.Delete(ctx, id)
}
