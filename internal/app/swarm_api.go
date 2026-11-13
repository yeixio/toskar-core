package app

import (
	"context"
	"errors"
	"strings"

	"github.com/yeixio/toskar-core/internal/swarm"
	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// swarmTurn runs a full swarm for swarm:<profile> on the raw formats, and
// for MCP's run_swarm (#1135): a question swarm on prompt with the profile,
// whatever its strategy, in the mode ctx asks for (swarm.WithMode), else
// the profile's, else split. A swarm keeps its agents in chats, and an API
// request has none, so the coordinator's chat is a new one, with the
// request's earlier messages, which the swarm's project, named after the
// job, takes in as the engine does. It returns the coordinator's answer as
// it streams and its chat's id.
func (a *App) swarmTurn(ctx context.Context, ref, prompt string) (<-chan pluginapi.ChatChunk, string, error) {
	if !maySwarm(ctx) {
		return nil, "", ErrSwarmRole
	}
	if a.Swarms == nil || a.Conversations == nil {
		return nil, "", errors.New("swarms are not available")
	}
	profile, err := a.swarmProfile(ctx, ref)
	if err != nil {
		return nil, "", err
	}
	mode := swarm.ModeFor(swarm.ModeFrom(ctx), profile.Orchestration.Swarm, nil)
	conv, err := a.Conversations.Create(ctx, swarm.JobName(prompt), profile.ID, "")
	if err != nil {
		return nil, "", err
	}
	if o := turnopts.From(ctx); o != nil {
		for _, m := range o.History {
			if (m.Role == "user" || m.Role == "assistant") && strings.TrimSpace(m.Content) != "" {
				if _, err := a.Conversations.AddMessage(ctx, conv.ID, m.Role, m.Content); err != nil {
					return nil, "", err
				}
			}
		}
	}
	stream, err := a.RunChat(swarm.WithMode(ctx, mode), profile.ID, conv.ID, prompt, true, "", "")
	if err != nil {
		return nil, "", err
	}
	out := make(chan pluginapi.ChatChunk, cap(stream))
	go func() {
		defer close(out)
		failed := false
		for c := range stream {
			if c.Error != "" {
				failed = true
			}
			out <- c
		}
		a.settleSwarm(context.WithoutCancel(ctx), conv.ID, failed)
	}()
	return out, conv.ID, nil
}

// settleSwarm finishes the record of the swarm a chat's turn ran, when the
// turn ended without finishing it, such as one that answered as one agent.
func (a *App) settleSwarm(ctx context.Context, conversationID string, failed bool) {
	conv, err := a.Conversations.Get(ctx, conversationID)
	if err != nil || conv.SwarmID == "" {
		return
	}
	rec, err := a.Swarms.Get(ctx, conv.SwarmID)
	if err != nil || (rec.Status != swarm.StatusRunning && rec.Status != swarm.StatusMerging) {
		return
	}
	rec.Status = swarm.StatusDone
	if failed {
		rec.Status = swarm.StatusFailed
	}
	_ = a.Swarms.Save(ctx, rec)
}
