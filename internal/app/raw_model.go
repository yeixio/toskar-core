package app

import (
	"context"
	"fmt"

	"github.com/yeixio/toskar-core/internal/api/openai"
	"github.com/yeixio/toskar-core/internal/share"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// rawModel readies a model installed on this computer for a raw request
// (#843): one that drives the model directly, with its own tools, as coding
// agents do. The model runs with a window of at least need tokens where one
// fits, as the API's other requests get (#962). It returns the model's
// llama-server and a func that ends the request's share of the computer.
func (a *App) rawModel(ctx context.Context, modelID string, need int) (string, func(), error) {
	if a.Models == nil {
		return "", nil, openai.ErrRawNotHere
	}
	if _, err := a.Models.Path(ctx, modelID); err != nil {
		return "", nil, openai.ErrRawNotHere
	}
	if window := a.knownWindow(ctx, modelID); window > 0 && need > window {
		if a.growWindow(ctx, modelID, need) == 0 {
			return "", nil, fmt.Errorf("%w: the request is about %d tokens, more than %s can hold on this computer (%d)", openai.ErrRawTooLong, need, modelID, window)
		}
	}
	// A raw request is someone waiting, as a chat is, so training waits.
	work, _ := a.enterWork(ctx, share.Interactive, "API", nil)
	done := func() { work.Done() }
	endpoint, err := a.ensureLocalModel(ctx, modelID)
	if err != nil {
		done()
		return "", nil, err
	}
	a.markModelUsed(modelID)
	return endpoint, done, nil
}

// rawModels are the chat models installed on this computer, the ones a raw
// request can drive.
func (a *App) rawModels(ctx context.Context) []string {
	if a.Models == nil {
		return nil
	}
	list, err := a.Models.List(ctx)
	if err != nil {
		return nil
	}
	var ids []string
	for _, m := range list {
		if m.Installed && contracts.SupportRoleOf(m) == "" {
			ids = append(ids, m.ID)
		}
	}
	return ids
}
