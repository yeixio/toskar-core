package app

import (
	"context"
	"sort"

	"github.com/yeixio/toskar-core/internal/egress"
	"github.com/yeixio/toskar-core/internal/events"
	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/internal/linkcheck"
	"github.com/yeixio/toskar-core/internal/locale"
	"github.com/yeixio/toskar-core/internal/profiles"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// settingCheckLinks asks each linked site whether an answer's links open
// (#209). Off by default: it contacts every site an answer links to.
const settingCheckLinks = "check_links"

// mayCheckLinks reports whether an answer's links may be checked: the
// setting is on, the profile may use the internet, and the turn used
// nothing marked this computer only.
func (a *App) mayCheckLinks(ctx context.Context, profile profiles.Profile, localOnly bool) bool {
	if a.Settings == nil || localOnly {
		return false
	}
	if on, _ := a.Settings.GetBool(ctx, settingCheckLinks, false); !on {
		return false
	}
	return tools.PolicyForProfile(profile, "internet.open") != tools.PolicyDeny
}

// checkAnswerLinks asks each site an answer links to whether the page
// opens, once the answer is saved, so it never waits on them. What didn't
// open, or couldn't be judged, is added to the saved answer, with a notice
// when it has none, and chat.links_checked tells the apps to show it.
func (a *App) checkAnswerLinks(ctx context.Context, lang, conversationID, messageID, answer string) {
	links := huginn.Links(answer)
	if len(links) == 0 {
		return
	}
	found := a.linkChecker.Check(ctx, links, func(host string) {
		if a.Egress != nil {
			a.Egress.Add(ctx, egress.LinkCheck, host, "whether a page linked in an answer opens")
		}
	})
	var result contracts.LinkCheck
	for link, s := range found {
		switch s {
		case linkcheck.Dead:
			result.Dead = append(result.Dead, link)
		case linkcheck.Unknown:
			result.Unknown = append(result.Unknown, link)
		}
	}
	sort.Strings(result.Dead)
	sort.Strings(result.Unknown)
	if len(result.Dead) == 0 && len(result.Unknown) == 0 {
		return
	}
	err := a.Conversations.UpdateMessageMeta(ctx, conversationID, messageID, func(m *contracts.MessageMeta) {
		m.LinkCheck = &result
		if m.Notice == "" && len(result.Dead) > 0 {
			m.Notice = locale.T(lang, "chat:notices.deadLinks", map[string]any{"links": listIn(lang, result.Dead)})
		}
	})
	if err != nil {
		if a.Logger != nil {
			a.Logger.Warn("save checked links", "message_id", messageID, "error", err)
		}
		return
	}
	a.publish(ctx, events.New(events.ChatLinksChecked, map[string]any{
		"conversation_id": conversationID, "message_id": messageID, "dead": result.Dead, "unknown": result.Unknown,
	}))
}
