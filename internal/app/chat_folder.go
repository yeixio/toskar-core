package app

import (
	"context"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/runlog"
	"github.com/yeixio/toskar-core/internal/turnopts"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// chatFolder is where a chat's turn works (#1017). A terminal on this computer
// says, and the chat remembers, so the task goes on from the web or desktop.
// There, the chat's folder, or else its project's (#1136), applies for an
// Admin, who can run things on this computer anyway. A phone, the watch, and
// the car answer as an ordinary chat: they can't show a task's questions and
// output as these can.
func (a *App) chatFolder(ctx context.Context, conv contracts.Conversation, project *contracts.Project) context.Context {
	if named := turnopts.ProjectFrom(ctx); named != "" {
		if conv.ID != "" && named != conv.Project {
			_ = a.Conversations.SetProject(ctx, conv.ID, named)
		}
		return ctx
	}
	folder := conv.Project
	if folder == "" && project != nil {
		folder = project.Folder
	}
	if folder == "" || !auth.PrincipalFrom(ctx).Person.Role.AtLeast(auth.RoleAdmin) {
		return ctx
	}
	switch runlog.OriginFrom(ctx).Client {
	case "web", "desktop", "terminal":
		if dir := turnopts.Project(folder); dir != "" {
			return turnopts.WithProject(ctx, dir)
		}
	}
	return ctx
}
