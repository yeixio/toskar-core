package app

import (
	"context"
	"regexp"
	"slices"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/huginn"
	"github.com/yeixio/toskar-core/internal/locale"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// A better-fitting model for a request (#707). After an answer, when a
// model this computer can run would have answered this kind of request
// better, and none that's installed would, the answer carries an offer to
// install it and answer again: for coding, the person's files or
// knowledge, a language the model writes weakly, math and logic, or a
// chat longer than the model reads at once. The answer itself stands.

// Reasons for an offer, which a chat gets at most once each.
const (
	offerCoding    = "coding"
	offerData      = "data"
	offerLanguage  = "language"
	offerReasoning = "reasoning"
	offerContext   = "context"
)

// offerSignals is what an answer says about the request it answered.
type offerSignals struct {
	message  string
	kind     huginn.Kind
	effort   huginn.Effort
	lang     string // the answer's language
	weakLang bool   // the model writes it less well
	dataKind string // "file", "knowledge", or ""
	longChat bool   // older messages were summarized or left out
	answered string // the model that answered
}

// mathRe is a question that's math or logic, where a reasoning model helps.
var mathRe = regexp.MustCompile(`(?i)\d\s*[-+*/×÷^]\s*\d|\b(solve|prove|equation|integral|derivative|probability|how many ways|logic puzzle|riddle)\b`)

// modelOffer is the offer for an answer, or nil: only in chats, only for
// Admins and the Owner (who install models), only when the setting allows, once per
// reason in a chat, and only for a model that fits this computer well and
// has room on disk.
func (a *App) modelOffer(ctx context.Context, conversationID, appLang string, s offerSignals) *contracts.SetupOffer {
	if conversationID == "" || a.Models == nil {
		return nil
	}
	// Each person turns it off for themselves, in Settings › History.
	if a.personalString(ctx, "suggest_models", "true") == "false" {
		return nil
	}
	// Installing a model is for Admins and the Owner, who also choose
	// models; anyone else isn't offered one.
	if !auth.PrincipalFrom(ctx).Person.Role.AtLeast(auth.RoleAdmin) || !a.personCapabilities(ctx).Allows(auth.CapChooseModel) {
		return nil
	}
	all, err := a.Models.List(ctx)
	if err != nil {
		return nil
	}
	var answered contracts.Model
	var installed []contracts.Model
	for _, m := range all {
		if m.ID == s.answered {
			answered = m
		}
		if m.Installed && !huginn.Supporting(m) {
			installed = append(installed, m)
		}
	}
	if answered.ID == "" {
		return nil
	}
	reason, wanted := offerReason(s, answered, installed)
	if reason == "" || a.offeredInChat(ctx, conversationID, reason) {
		return nil
	}
	m, fit, ok := a.bestFitting(ctx, all, wanted, s.lang)
	if !ok || m.ID == answered.ID {
		return nil
	}
	params := map[string]any{"name": m.DisplayName, "model": huginn.Name(answered)}
	if reason == offerLanguage {
		params["language"] = locale.LanguageName(s.lang, appLang)
	}
	return &contracts.SetupOffer{
		Ability:   "model",
		Reason:    reason,
		Label:     locale.T(appLang, "chat:setup.model."+reason, params),
		Option:    m.ID,
		Name:      m.DisplayName,
		SizeBytes: int64(m.SizeBytes),
		Fit:       string(fit),
		Request:   s.message,
	}
}

// offerReason is why an answer could have been better, and which models
// would have answered it better; "" when nothing installed falls short.
func offerReason(s offerSignals, answered contracts.Model, installed []contracts.Model) (string, func(contracts.Model) bool) {
	switch {
	case s.kind == huginn.Coding && !slices.ContainsFunc(installed, codingModel):
		return offerCoding, codingModel
	case s.dataKind != "" && huginn.Small(answered) && !slices.ContainsFunc(installed, largerGeneral):
		return offerData, largerGeneral
	case s.weakLang && s.lang != "":
		return offerLanguage, func(m contracts.Model) bool { return generalModel(m) && writesWell(m, s.lang) }
	case s.effort == huginn.EffortThorough && mathRe.MatchString(s.message) && !slices.ContainsFunc(installed, reasoningModel):
		return offerReasoning, reasoningModel
	case s.longChat && answered.Context > 0:
		return offerContext, func(m contracts.Model) bool { return generalModel(m) && m.Context >= 2*answered.Context }
	}
	return "", nil
}

func codingModel(m contracts.Model) bool {
	return m.Capabilities.Coding || slices.Contains(m.Tags, "coding")
}

func reasoningModel(m contracts.Model) bool { return slices.Contains(m.Tags, "reasoning") }

// generalModel answers chats: not a supporting model, and not one only
// for pictures or only for reasoning.
func generalModel(m contracts.Model) bool {
	if huginn.Supporting(m) {
		return false
	}
	if slices.Contains(m.Tags, "general") {
		return true
	}
	return !slices.Contains(m.Tags, "vision") && !slices.Contains(m.Tags, "reasoning")
}

func largerGeneral(m contracts.Model) bool { return generalModel(m) && !huginn.Small(m) }

func writesWell(m contracts.Model, lang string) bool {
	l, ok := huginn.LanguageLevel(m, lang)
	return ok && (l.Level == "good" || l.Level == "excellent")
}

// bestFitting is the model to offer: not installed, wanted, fitting this
// computer excellently or well, with room on disk; the one that writes the
// language best, then the largest, as a stand-in for quality.
func (a *App) bestFitting(ctx context.Context, all []contracts.Model, wanted func(contracts.Model) bool, lang string) (contracts.Model, contracts.FitLabel, bool) {
	hw, err := a.detectHardware(ctx)
	if err != nil {
		return contracts.Model{}, "", false
	}
	cfg := a.Config.Get()
	running, _ := a.listRunningAll(ctx)
	fits := map[string]contracts.FitLabel{}
	for _, f := range a.Models.Fit(hw, cfg.NodeID, cfg.NodeName, a.fitOptions(ctx, cfg.NodeID, true, running)).Fits {
		fits[f.ModelID] = f.Label
	}
	var best contracts.Model
	var bestFit contracts.FitLabel
	for _, m := range all {
		fit := fits[m.ID]
		if m.Installed || !wanted(m) || (fit != contracts.FitExcellent && fit != contracts.FitGood) {
			continue
		}
		// Room for it, with a tenth to spare.
		if free := hw.Disk.AvailableBytes; free > 0 && m.SizeBytes > 0 && m.SizeBytes+m.SizeBytes/10 > free {
			continue
		}
		if best.ID == "" || better(m, best, lang) {
			best, bestFit = m, fit
		}
	}
	return best, bestFit, best.ID != ""
}

func better(a, b contracts.Model, lang string) bool {
	if lang != "" {
		if ra, rb := languageLevelRank(a, lang), languageLevelRank(b, lang); ra != rb {
			return ra > rb
		}
	}
	return a.MemoryNeeded > b.MemoryNeeded
}

func languageLevelRank(m contracts.Model, lang string) int {
	l, ok := huginn.LanguageLevel(m, lang)
	if !ok {
		return 0
	}
	return map[string]int{"limited": 1, "fair": 2, "good": 3, "excellent": 4}[l.Level]
}

// offeredInChat reports an offer for this reason already in the chat.
func (a *App) offeredInChat(ctx context.Context, conversationID, reason string) bool {
	msgs, err := a.Conversations.ListMessages(ctx, conversationID)
	if err != nil {
		return true
	}
	for _, m := range msgs {
		if m.Meta != nil && m.Meta.Setup != nil && m.Meta.Setup.Ability == "model" && m.Meta.Setup.Reason == reason {
			return true
		}
	}
	return false
}
