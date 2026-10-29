package app

import (
	"context"
	"time"
	"unicode/utf8"

	"github.com/yeixio/toskar-core/internal/scheduler"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// chatPlace is where a chat's last turn ran (#836): that computer has the
// chat in its prompt cache, about tokens of it.
type chatPlace struct {
	node, model string
	tokens      int
	at          time.Time
}

// maxChatPlaces bounds chatPlaces; the oldest go first. A cache older than
// chatPlaceTTL has likely been replaced by other chats.
const (
	maxChatPlaces = 512
	chatPlaceTTL  = time.Hour
)

// rememberPlace notes that a chat's turn ran on node, tokens long.
func (a *App) rememberPlace(conversationID, node, model string, tokens int) {
	if conversationID == "" || node == "" || tokens <= 0 {
		return
	}
	a.placesMu.Lock()
	defer a.placesMu.Unlock()
	if a.chatPlaces == nil {
		a.chatPlaces = map[string]chatPlace{}
	}
	a.chatPlaces[conversationID] = chatPlace{node: node, model: model, tokens: tokens, at: time.Now()}
	for len(a.chatPlaces) > maxChatPlaces {
		oldest := ""
		for id, p := range a.chatPlaces {
			if oldest == "" || p.at.Before(a.chatPlaces[oldest].at) {
				oldest = id
			}
		}
		delete(a.chatPlaces, oldest)
	}
}

// previousTurn is where a chat's last turn ran, or nil.
func (a *App) previousTurn(conversationID string) *scheduler.PreviousTurn {
	if conversationID == "" {
		return nil
	}
	a.placesMu.Lock()
	defer a.placesMu.Unlock()
	p, ok := a.chatPlaces[conversationID]
	if !ok || time.Since(p.at) > chatPlaceTTL {
		return nil
	}
	return &scheduler.PreviousTurn{NodeID: p.node, ModelID: p.model, Tokens: p.tokens}
}

// placementRequest is what placement knows about the request it places.
type placementRequest struct {
	promptTokens int
	previous     *scheduler.PreviousTurn
}

type placementKey struct{}

// withPlacementRequest tells placement the request's size and where its
// chat ran before.
func withPlacementRequest(ctx context.Context, r placementRequest) context.Context {
	return context.WithValue(ctx, placementKey{}, r)
}

func placementRequestFrom(ctx context.Context) placementRequest {
	r, _ := ctx.Value(placementKey{}).(placementRequest)
	return r
}

type followUpKey struct{}

// withFollowUp marks a model call that continues a chat on the computer
// that served its last turn, so its cached tokens measure cache hits.
func withFollowUp(ctx context.Context) context.Context {
	return context.WithValue(ctx, followUpKey{}, true)
}

func isFollowUp(ctx context.Context) bool {
	v, _ := ctx.Value(followUpKey{}).(bool)
	return v
}

// tokensOf estimates text's tokens at four characters each, close enough
// for placement to tell a long request from a short one.
func tokensOf(text string) int {
	return (utf8.RuneCountInString(text) + 3) / 4
}

// promptTokens estimates the tokens of messages.
func promptTokens(messages []pluginapi.ChatMessage) int {
	n := 0
	for _, m := range messages {
		n += tokensOf(m.Content) + 4
	}
	return n
}

// observeSpeed adds an answer's measurements to the speeds placement
// predicts from.
func (a *App) observeSpeed(ctx context.Context, node, model string, m *pluginapi.GenerationMetrics) {
	if m == nil {
		return
	}
	a.speeds.Observe(node, model, scheduler.Sample{
		PromptTokens: m.PromptTokens, CachedTokens: m.CachedTokens, CompletionTokens: m.CompletionTokens,
		PrefillPerSec: m.PromptTokPerSec, DecodePerSec: m.EvalTokPerSec,
		FollowUp: isFollowUp(ctx),
	})
}

// rememberTurnPlace passes a chat turn's stream through and, when it ends,
// notes where it ran and how long the chat now is there.
func (a *App) rememberTurnPlace(in <-chan pluginapi.ChatChunk, conversationID, node, model string) <-chan pluginapi.ChatChunk {
	if conversationID == "" {
		return in
	}
	out := make(chan pluginapi.ChatChunk, cap(in))
	go func() {
		defer close(out)
		var m *pluginapi.GenerationMetrics
		for c := range in {
			if c.Metrics != nil {
				m = c.Metrics
			}
			out <- c
		}
		if m != nil {
			a.rememberPlace(conversationID, node, model, m.PromptTokens+m.CompletionTokens)
		}
	}()
	return out
}
