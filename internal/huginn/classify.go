// Package huginn decides how a chat request should run: what kind of request
// it is, which installed model answers it when the user picks Auto, and which
// model to fall back to when that one fails.
package huginn

import (
	"github.com/yeixio/toskar-core/internal/typo"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/yeixio/toskar-core/internal/locale"
	"github.com/yeixio/toskar-core/internal/tools"
)

// Kind is the class of a request (AI experience spec §12).
type Kind string

const (
	// Chat is a simple question or conversation.
	Chat Kind = "chat"
	// Current needs information that changes: news, prices, weather, versions.
	Current Kind = "current"
	// Coding is about writing, reading, or fixing code.
	Coding Kind = "coding"
	// Research needs a long or careful answer: comparisons, plans, analysis.
	Research Kind = "research"
	// Local acts on this computer: files, commands, Git.
	Local Kind = "local"
)

// Describe is a plain-language name for a kind in lang, used in "Auto
// chose … for …".
func (k Kind) Describe(lang string) string {
	switch k {
	case Current, Coding, Research, Local:
		return locale.T(lang, "chat:steps.kinds."+string(k), nil)
	default:
		return locale.T(lang, "chat:steps.kinds.chat", nil)
	}
}

var (
	localRe    = regexp.MustCompile(`(?i)\b(my (files?|folders?|desktop|downloads|documents|repo|repository|project)|this (folder|directory|repo|repository|project)|run (the |a )?(command|script|tests?)|in (the )?terminal|git (status|log|diff|commit|push|pull|branch)|commit (my|the|these) changes|read the file|open the file|save (it|this) (as|to) )`)
	codingRe   = regexp.MustCompile("(?i)(```|\\b((my|this|the|source|sample|example) code|code (snippet|review|example)|stack ?trace|exception|compile[rsd]?|refactor|regex|regular expression|sql|unit tests?|python|golang|javascript|typescript|java|kotlin|php|bash script|shell script|html|css|json|yaml|dockerfile|kubernetes|syntax error|null pointer|segfault|bug in (my|the|this)|react (component|hook|app)|rust (code|program|crate)|swiftui|api endpoint|write a (function|script|program|class))\\b|\\b[a-z_]+\\.[a-z_]+\\(|=>|:=|\\bfunc |\\bdef |#include|\\bc(\\+\\+|#)\\s)")
	researchRe = regexp.MustCompile(`(?i)\b(compare|comparison|versus|vs\.?|pros and cons|trade-?offs?|analy[sz]e|analysis|in detail|detailed|step[- ]by[- ]step|plan for|make a plan|write an? (essay|report|proposal|memo|article)|research|evaluate|recommend(ation)?s? for|strategy|outline)\b`)
)

// briefLength is the message length, in runes, up to which a question that
// needs the web or this computer is short enough to answer with a model
// already loaded (#498), such as "weather in Juneau tomorrow?".
const briefLength = 120

// Brief reports whether a request wants a short answer soon, whatever model
// would answer it best: a short question that needs the web or this
// computer, or any question from a watch, where every second of loading
// shows (#498). client is the app that asked, as in runlog.Origin. A quick
// chat question is already quick, and coding and research are never brief
// but from a watch.
func Brief(k Kind, message, client string) bool {
	if client == "watch" {
		return true
	}
	return (k == Current || k == Local) && utf8.RuneCountInString(strings.TrimSpace(message)) <= briefLength
}

// researchLength is the message length, in runes, past which a request is
// treated as needing a careful answer.
const researchLength = 600

// classifyWords are the words the rules below match, for typos.
var classifyWords = typo.FromPatterns(localRe.String(), codingRe.String(), researchRe.String())

// Classify sorts a request. The order matters: acting on this computer and
// current information decide what the turn needs to do, so they win over the
// subject of the question.
func Classify(message string) Kind {
	msg := strings.TrimSpace(message)
	if msg == "" {
		return Chat
	}
	// The rules' words with typos corrected (#451); a match on what was
	// typed counts first.
	fixed := classifyWords.Fix(msg)
	if localRe.MatchString(msg) || localRe.MatchString(fixed) {
		return Local
	}
	if tools.MessageNeedsLiveWeb(msg) {
		return Current
	}
	if codingRe.MatchString(msg) || codingRe.MatchString(fixed) {
		return Coding
	}
	if researchRe.MatchString(msg) || researchRe.MatchString(fixed) || utf8.RuneCountInString(msg) > researchLength {
		return Research
	}
	return Chat
}
