package app

import (
	"context"

	"github.com/yeixio/toskar-core/internal/projects"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// projectInstructionsHeading starts a project's instructions in a turn.
const projectInstructionsHeading = "Project instructions, from the user, for every chat in this project:\n"

// chatProject is the project a turn is in (#1136): the chat's, or the one
// the request asked for. A chat not yet in a project joins the one asked
// for; one already in a project stays in it. Without a chat, the turn uses
// the project without saving one.
func (a *App) chatProject(ctx context.Context, conv *contracts.Conversation) *contracts.Project {
	if a.Projects == nil {
		return nil
	}
	id := conv.ProjectID
	asked := projects.IDFrom(ctx)
	if id == "" {
		id = asked
	}
	if id == "" {
		return nil
	}
	p, err := a.Projects.Get(ctx, id)
	if err != nil {
		return nil
	}
	if conv.ID != "" && conv.ProjectID == "" {
		if err := a.Conversations.SetProjectID(ctx, conv.ID, p.ID); err != nil {
			return nil
		}
		conv.ProjectID = p.ID
	}
	return &p
}
