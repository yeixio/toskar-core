package app

import (
	"context"
	"errors"
	"strings"

	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/internal/muninn"
	"github.com/yeixio/toskar-core/internal/runtimes/llamacpp"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Auto-compact (#342): long chats send their older messages as a summary,
// and every message stays saved. The person can read and correct the
// summary, run it now, keep a chat verbatim, start a new chat from it, and
// choose how early it starts (the compact_at setting).

// ErrNothingToCompact is a chat too short to summarize.
var ErrNothingToCompact = contracts.NewError("NOTHING_TO_COMPACT", nil, errors.New("this chat is too short to compact"))

// ErrNoSummary is a chat that hasn't been compacted.
var ErrNoSummary = contracts.NewError("SUMMARY_NOT_FOUND", nil, errors.New("this chat hasn't been compacted"))

// compactAt is the share of a model's window at which auto-compact starts,
// from Settings › Chat.
func (a *App) compactAt(ctx context.Context) float64 {
	setting := ""
	if a.Settings != nil {
		setting, _ = a.Settings.GetString(ctx, "compact_at", "normal")
	}
	return muninn.CompactAt(setting)
}

// compactOff reports a chat kept verbatim.
func (a *App) compactOff(ctx context.Context, conversationID string) bool {
	if a.Conversations == nil || conversationID == "" {
		return false
	}
	conv, err := a.Conversations.Get(ctx, conversationID)
	return err == nil && conv.CompactOff
}

// windowFor is a model's context window: its real one while it runs here,
// else the catalog's.
func (a *App) windowFor(ctx context.Context, modelID string) int {
	if window, _, ok := a.localContext(ctx, modelID); ok {
		return window
	}
	catalog := 0
	if a.Models != nil {
		if entry, ok := a.Models.Catalog().Get(modelID); ok {
			catalog = entry.Context
		}
	}
	return llamacpp.ContextWindow(catalog)
}

// compactModel is the model that summarizes a chat outside a turn: the
// chat's own, else the one that wrote its last summary, else the default.
func (a *App) compactModel(ctx context.Context, conversationID string) (string, error) {
	if conv, err := a.Conversations.Get(ctx, conversationID); err == nil && usableForCompact(conv.ModelID) {
		return conv.ModelID, nil
	}
	if sum, ok, err := a.Muninn.GetSummary(ctx, conversationID); err == nil && ok && usableForCompact(sum.ModelID) {
		return sum.ModelID, nil
	}
	return a.defaultInstalledModelID(ctx)
}

func usableForCompact(id string) bool {
	return id != "" && id != huginn.AutoModelID && !strings.HasPrefix(id, "sai:")
}

// compactJob is a summarizer run with modelID, forced when the person asked
// or a turn wouldn't fit.
func (a *App) compactJob(ctx context.Context, modelID string, force bool) *muninn.Summarizer {
	return &muninn.Summarizer{Store: a.Muninn, ModelID: modelID, Count: a.tokenCounter(ctx, modelID),
		At: a.compactAt(ctx), Force: force,
		Generate: func(ctx context.Context, msgs []pluginapi.ChatMessage) (string, error) {
			return a.generateOnce(ctx, modelID, "", a.localAdapters(ctx, modelID), msgs)
		}}
}

// ConversationSummary is a chat's summary, for the person to read.
func (a *App) ConversationSummary(ctx context.Context, conversationID string) (muninn.Summary, error) {
	if _, err := a.Conversations.Get(ctx, conversationID); err != nil {
		return muninn.Summary{}, err
	}
	sum, ok, err := a.Muninn.GetSummary(ctx, conversationID)
	if err != nil {
		return muninn.Summary{}, err
	}
	if !ok {
		return muninn.Summary{}, ErrNoSummary
	}
	return sum, nil
}

// EditConversationSummary saves the person's correction; later summaries
// keep it.
func (a *App) EditConversationSummary(ctx context.Context, conversationID, text string) (muninn.Summary, error) {
	sum, err := a.ConversationSummary(ctx, conversationID)
	if err != nil {
		return muninn.Summary{}, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return muninn.Summary{}, contracts.NewError("SUMMARY_EMPTY", nil, errors.New("a summary can't be empty"))
	}
	sum.Text, sum.Edited = text, true
	if err := a.Muninn.SaveSummary(ctx, sum); err != nil {
		return muninn.Summary{}, err
	}
	return a.ConversationSummary(ctx, conversationID)
}

// CompactConversation summarizes a chat's older messages now, whatever its
// size, keeping the newest verbatim.
func (a *App) CompactConversation(ctx context.Context, conversationID string) (muninn.Summary, error) {
	if a.Muninn == nil || a.summarizer == nil {
		return muninn.Summary{}, errors.New("auto-compact isn't available")
	}
	if _, err := a.Conversations.Get(ctx, conversationID); err != nil {
		return muninn.Summary{}, err
	}
	modelID, err := a.compactModel(ctx, conversationID)
	if err != nil {
		return muninn.Summary{}, err
	}
	stored, err := a.Conversations.ListMessages(ctx, conversationID)
	if err != nil {
		return muninn.Summary{}, err
	}
	ran, err := a.summarizer.RunWith(ctx, a.compactJob(ctx, modelID, true), conversationID, stored, a.windowFor(ctx, modelID))
	if err != nil {
		return muninn.Summary{}, err
	}
	if !ran {
		if sum, ok, _ := a.Muninn.GetSummary(ctx, conversationID); ok {
			return sum, nil
		}
		return muninn.Summary{}, ErrNothingToCompact
	}
	return a.ConversationSummary(ctx, conversationID)
}

// ConversationFromSummary starts a new chat that begins with another's
// summary, with the same profile and model.
func (a *App) ConversationFromSummary(ctx context.Context, conversationID string) (contracts.Conversation, error) {
	from, err := a.Conversations.Get(ctx, conversationID)
	if err != nil {
		return contracts.Conversation{}, err
	}
	sum, err := a.ConversationSummary(ctx, conversationID)
	if err != nil {
		return contracts.Conversation{}, err
	}
	title := from.Title
	if !strings.HasSuffix(title, " (continued)") {
		title += " (continued)"
	}
	conv, err := a.Conversations.Create(ctx, title, from.ProfileID, from.ModelID)
	if err != nil {
		return contracts.Conversation{}, err
	}
	// The summary opens the chat, as the person's own message, so it's
	// there to read and the model sees it as context.
	if _, err := a.Conversations.AddMessage(ctx, conv.ID, "user", "Summary of an earlier chat, \""+from.Title+"\":\n\n"+sum.Text); err != nil {
		return contracts.Conversation{}, err
	}
	if _, err := a.Conversations.AddMessage(ctx, conv.ID, "assistant", "Thanks. I have the earlier context; let's continue."); err != nil {
		return contracts.Conversation{}, err
	}
	return a.Conversations.Get(ctx, conv.ID)
}
